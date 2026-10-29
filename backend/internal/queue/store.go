package queue

import (
	"context"
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
	// Recovered: the row was 'sending' with an expired lease, i.e. a worker
	// died mid-send and the earlier attempt's outcome is unknown.
	Recovered bool
	// HasConsent is the user's consent for Channel as read at claim time.
	HasConsent bool
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
            m.body, m.attempts, picked.prev_status = 'sending' as recovered
)
select c.id, c.campaign_id, c.user_id, c.channel, c.recipient, c.subject, c.body, c.attempts, c.recovered,
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
			&j.Attempts, &j.Recovered, &j.HasConsent)
		return j, err
	})
}

// All Mark* updates only touch rows still in 'sending', so a late update from
// a worker that lost its lease cannot overwrite a terminal state.

// MarkSent records a successful (or provider-deduplicated) send.
func (s *Store) MarkSent(ctx context.Context, id uuid.UUID, providerID string) error {
	_, err := s.pool.Exec(ctx, `update messages set status='sent', provider_message_id=nullif($2,''), sent_at=now(),
		locked_until=null, last_error=null, updated_at=now() where id=$1 and status='sending'`, id, providerID)
	return err
}

// MarkRetry re-queues the message for at. refundAttempt gives back the
// attempt taken by Claim (429s and never-started sends do not count).
func (s *Store) MarkRetry(ctx context.Context, id uuid.UUID, at time.Time, refundAttempt bool, lastErr string) error {
	_, err := s.pool.Exec(ctx, `update messages set status='queued', next_attempt_at=$2,
		attempts = case when $3 then greatest(attempts-1, 0) else attempts end,
		locked_until=null, last_error=$4, updated_at=now() where id=$1 and status='sending'`, id, at, refundAttempt, lastErr)
	return err
}

// MarkFailed: certainly not delivered and will not be retried.
func (s *Store) MarkFailed(ctx context.Context, id uuid.UUID, lastErr string) error {
	return s.markTerminal(ctx, id, "failed", lastErr)
}

// MarkUnknown: may or may not have been delivered; will not be retried.
func (s *Store) MarkUnknown(ctx context.Context, id uuid.UUID, lastErr string) error {
	return s.markTerminal(ctx, id, "unknown", lastErr)
}

// MarkSkipped: consent was revoked before sending.
func (s *Store) MarkSkipped(ctx context.Context, id uuid.UUID) error {
	return s.markTerminal(ctx, id, "skipped_no_consent", "consent revoked before send")
}

func (s *Store) markTerminal(ctx context.Context, id uuid.UUID, status, lastErr string) error {
	_, err := s.pool.Exec(ctx, `update messages set status=$2, last_error=$3, locked_until=null, updated_at=now()
		where id=$1 and status='sending'`, id, status, lastErr)
	return err
}
