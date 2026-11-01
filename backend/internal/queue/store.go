package queue

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Job is a claimed message. After Claim the row is status='sending' with a
// lease (locked_until) and attempts already incremented.
type Job struct {
	ID, CampaignID uuid.UUID
	UserID         int64
	Channel        string
	Recipient      string
	Subject        string
	Body           string
	Attempts       int
	// RateLimited counts earlier 429 answers for this message.
	RateLimited int
	// Recovered: the row was 'sending' with an expired lease, i.e. a worker
	// died mid-send and the earlier attempt's outcome is unknown.
	Recovered bool
	// HasConsent is the user's consent for Channel as read at claim time.
	HasConsent bool
	// LockedUntil is the lease set by this claim. It doubles as a fencing
	// token for the Mark* updates.
	LockedUntil time.Time
}

// Store is the queue's data access on top of the messages table.
type Store struct{ pool *pgxpool.Pool }

// NewStore returns a Store using pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// claimSQL is spec §10.1 plus: the previous status (to detect crash recovery)
// and the user's *current* consent for the channel, read in the same
// statement that hands the job to the sender. This is the "consent is checked
// again immediately before sending" step (DECISIONS #5).
const claimSQL = `
with picked as (
  select m.id, m.status as prev_status
  from messages m
  where m.channel = $1
    and (m.status = 'queued' or (m.status = 'sending' and m.locked_until < now()))
    and m.next_attempt_at <= now()
  order by m.next_attempt_at
  limit $2
  for update skip locked
), claimed as (
  update messages m
  set status = 'sending',
      locked_until = now() + make_interval(secs => $3),
      attempts = m.attempts + 1,
      updated_at = now()
  from picked
  where m.id = picked.id
  returning m.id, m.campaign_id, m.user_id, m.channel, m.recipient, coalesce(m.subject, '') as subject,
            m.body, m.attempts, m.rate_limited, picked.prev_status = 'sending' as recovered, m.locked_until
)
select c.id, c.campaign_id, c.user_id, c.channel, c.recipient, c.subject, c.body, c.attempts, c.rate_limited, c.recovered, c.locked_until,
       coalesce(case c.channel when 'sms' then u.sms_consent else u.email_consent end, false) as has_consent
from claimed c join users u on u.id = c.user_id`

// Claim atomically takes up to limit due messages of channel and leases them
// for lease. Concurrent callers never receive the same row (SKIP LOCKED).
// Rows stuck in 'sending' past their lease are reclaimed with Recovered=true.
func (s *Store) Claim(ctx context.Context, channel string, limit int, lease time.Duration) ([]Job, error) {
	rows, err := s.pool.Query(ctx, claimSQL, channel, limit, lease.Seconds())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Job, error) {
		var j Job
		err := r.Scan(&j.ID, &j.CampaignID, &j.UserID, &j.Channel, &j.Recipient, &j.Subject, &j.Body,
			&j.Attempts, &j.RateLimited, &j.Recovered, &j.LockedUntil, &j.HasConsent)
		return j, err
	})
}

// ErrLeaseLost is returned by the Mark* methods when the row is no longer
// held under the job's lease (it expired and was reclaimed, or was already
// finalized). The update is then a no-op.
var ErrLeaseLost = errors.New("queue: lease lost; row not updated")

// All Mark* updates are fenced by the claim's lease token: they only touch the
// row if it is still 'sending' with the exact locked_until set by the Claim
// that produced j. A stale in-process copy of a job (lease expired and the
// row reclaimed) therefore cannot overwrite the newer claim's outcome.
const fence = ` where id=$1 and status='sending' and locked_until=$2`

func (s *Store) exec(ctx context.Context, sql string, args ...any) error {
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// MarkSent records a successful (or provider-deduplicated) send. last_error
// is deliberately kept: a message that was retried and then sent still shows
// its retry history (with status 'sent') in the dashboard's recent errors.
func (s *Store) MarkSent(ctx context.Context, j Job, providerID string) error {
	return s.exec(ctx, `update messages set status='sent', provider_message_id=nullif($3,''), sent_at=now(),
		locked_until=null, updated_at=now()`+fence, j.ID, j.LockedUntil, providerID)
}

// MarkRetry re-queues the message for at. refundAttempt gives back the
// attempt taken by Claim (429s and never-started sends do not count).
func (s *Store) MarkRetry(ctx context.Context, j Job, at time.Time, refundAttempt bool, lastErr string) error {
	return s.exec(ctx, `update messages set status='queued', next_attempt_at=$3,
		attempts = case when $4 then greatest(attempts-1, 0) else attempts end,
		locked_until=null, last_error=$5, updated_at=now()`+fence, j.ID, j.LockedUntil, at, refundAttempt, lastErr)
}

// MarkRateLimited re-queues the message for at after a 429: the attempt is
// refunded (nothing was sent) and the 429 counter goes up.
func (s *Store) MarkRateLimited(ctx context.Context, j Job, at time.Time, lastErr string) error {
	return s.exec(ctx, `update messages set status='queued', next_attempt_at=$3,
		attempts = greatest(attempts-1, 0), rate_limited = rate_limited + 1,
		locked_until=null, last_error=$4, updated_at=now()`+fence, j.ID, j.LockedUntil, at, lastErr)
}

// MarkFailed: certainly not delivered and will not be retried.
func (s *Store) MarkFailed(ctx context.Context, j Job, lastErr string) error {
	return s.markTerminal(ctx, j, "failed", lastErr, false)
}

// MarkRateLimitedOut: the provider answered 429 too many times; certainly
// not delivered, given up. The attempt is refunded like any 429.
func (s *Store) MarkRateLimitedOut(ctx context.Context, j Job, lastErr string) error {
	return s.exec(ctx, `update messages set status='failed', last_error=$3, locked_until=null, updated_at=now(),
		attempts = greatest(attempts-1, 0), rate_limited = rate_limited + 1`+fence, j.ID, j.LockedUntil, lastErr)
}

// MarkUnknown: may or may not have been delivered; will not be retried.
func (s *Store) MarkUnknown(ctx context.Context, j Job, lastErr string) error {
	return s.markTerminal(ctx, j, "unknown", lastErr, false)
}

// MarkSkipped: consent was revoked before sending. Nothing was sent, so the
// attempt taken by Claim is refunded.
func (s *Store) MarkSkipped(ctx context.Context, j Job) error {
	return s.markTerminal(ctx, j, "skipped_no_consent", "consent revoked before send", true)
}

func (s *Store) markTerminal(ctx context.Context, j Job, status, lastErr string, refund bool) error {
	return s.exec(ctx, `update messages set status=$3, last_error=$4, locked_until=null, updated_at=now(),
		attempts = case when $5 then greatest(attempts-1, 0) else attempts end`+fence,
		j.ID, j.LockedUntil, status, lastErr, refund)
}
