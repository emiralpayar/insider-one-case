package campaign

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/content"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrAlreadyApproved means the campaign is already sending or completed.
	ErrAlreadyApproved = errors.New("campaign: already approved")
	// ErrEmptyAudience means no consented user would receive a message.
	ErrEmptyAudience = errors.New("campaign: audience is empty")
	// ErrLargeAudienceNotConfirmed means the audience is >50% of all users and
	// the request did not confirm it.
	ErrLargeAudienceNotConfirmed = errors.New("campaign: large audience not confirmed")
)

// insertBatch is the number of message rows per INSERT statement.
const insertBatch = 500

// ApproveRequest is the approval input.
type ApproveRequest struct {
	ConfirmLargeAudience bool `json:"confirm_large_audience"`
}

// ApproveResult reports how many messages were queued per enabled channel.
type ApproveResult struct {
	Enqueued map[string]int `json:"enqueued"`
}

// Approve re-validates the rule and content, renders one message per
// consented user and enabled channel, queues them and moves the campaign to
// 'sending' — all in one transaction. The campaign row is locked FOR UPDATE so
// concurrent approvals serialize: the second one sees 'sending' and gets
// ErrAlreadyApproved. Inserts use ON CONFLICT DO NOTHING as a second guard
// against duplicates.
//
// Approve uses exactly one pool connection (the transaction's).
func Approve(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, req ApproveRequest) (ApproveResult, error) {
	asOf := time.Now()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ApproveResult{}, err
	}
	defer tx.Rollback(ctx)

	c, err := scan(tx.QueryRow(ctx, `select `+selectCols+` from campaigns where id=$1 for update`, id))
	if err != nil {
		return ApproveResult{}, err
	}
	switch {
	case Locked(c.Status):
		return ApproveResult{}, ErrAlreadyApproved
	case c.Status != StatusContentReady || c.Rule == nil || c.Content == nil:
		return ApproveResult{}, ErrNotReady
	}

	rule, errs := segment.Validate(*c.Rule)
	if len(errs) > 0 {
		return ApproveResult{}, errs
	}
	wc, err := WorstCaseFor(ctx, tx, rule, asOf)
	if err != nil {
		return ApproveResult{}, err
	}
	if errs := content.Validate(*c.Content, c.Channels, wc); len(errs) > 0 {
		return ApproveResult{}, errs
	}
	// The size decision uses data read in this transaction (same snapshot as
	// the rows we queue) and never takes a second pool connection while the
	// campaign row is locked.
	users, err := audience(ctx, tx, rule, asOf)
	if err != nil {
		return ApproveResult{}, err
	}
	var allUsers int
	if err := tx.QueryRow(ctx, `select count(*) from users`).Scan(&allUsers); err != nil {
		return ApproveResult{}, err
	}
	for _, w := range segment.SizeWarnings(len(users), allUsers) {
		switch {
		case w.Code == "empty_audience":
			return ApproveResult{}, ErrEmptyAudience
		case w.Code == "large_audience" && !req.ConfirmLargeAudience:
			return ApproveResult{}, ErrLargeAudienceNotConfirmed
		}
	}
	res := ApproveResult{Enqueued: map[string]int{}}
	total := 0
	for _, ch := range c.Channels {
		n, err := enqueue(ctx, tx, c, ch, users)
		if err != nil {
			return ApproveResult{}, fmt.Errorf("enqueue %s: %w", ch, err)
		}
		res.Enqueued[ch] = n
		total += n
	}
	if total == 0 {
		return ApproveResult{}, ErrEmptyAudience
	}
	if _, err := tx.Exec(ctx, `update campaigns set status='sending', approved_at=now(), updated_at=now() where id=$1`, id); err != nil {
		return ApproveResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ApproveResult{}, err
	}
	return res, nil
}

type recipientUser struct {
	id                       int64
	firstName, city          string
	phone, email             string
	smsConsent, emailConsent bool
}

func audience(ctx context.Context, tx pgx.Tx, r segment.Rule, asOf time.Time) ([]recipientUser, error) {
	where, args, err := segment.Compile(r, 0, asOf)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `select u.id, u.first_name, u.city, u.phone, u.email,
		coalesce(u.sms_consent, false), coalesce(u.email_consent, false)
		from users u where `+where+` order by u.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []recipientUser
	for rows.Next() {
		var u recipientUser
		if err := rows.Scan(&u.id, &u.firstName, &u.city, &u.phone, &u.email, &u.smsConsent, &u.emailConsent); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// enqueue renders and inserts the channel's messages for consented users.
func enqueue(ctx context.Context, tx pgx.Tx, c Campaign, ch string, users []recipientUser) (int, error) {
	var (
		ids        []uuid.UUID
		userIDs    []int64
		recipients []string
		subjects   []*string
		bodies     []string
	)
	inserted := 0
	flush := func() error {
		if len(ids) == 0 {
			return nil
		}
		tag, err := tx.Exec(ctx, `insert into messages (id, campaign_id, user_id, channel, recipient, subject, body, status, attempts)
			select t.id, $1, t.user_id, $2, t.recipient, t.subject, t.body, 'queued', 0
			from unnest($3::uuid[], $4::bigint[], $5::text[], $6::text[], $7::text[])
			     as t(id, user_id, recipient, subject, body)
			on conflict (campaign_id, user_id, channel) do nothing`,
			c.ID, ch, ids, userIDs, recipients, subjects, bodies)
		if err != nil {
			return err
		}
		inserted += int(tag.RowsAffected())
		ids, userIDs, recipients, subjects, bodies = ids[:0], userIDs[:0], recipients[:0], subjects[:0], bodies[:0]
		return nil
	}
	for _, u := range users {
		vars := map[string]string{"first_name": u.firstName, "city": u.city}
		var (
			recipient, body string
			subject         *string
		)
		switch ch {
		case "sms":
			if !u.smsConsent {
				continue
			}
			recipient, body = u.phone, content.Render(c.Content.SMS, vars)
		case "email":
			if !u.emailConsent {
				continue
			}
			s := content.Render(c.Content.Email.Subject, vars)
			recipient, subject, body = u.email, &s, content.Render(c.Content.Email.Body, vars)
		default:
			return 0, fmt.Errorf("unknown channel %q", ch)
		}
		ids = append(ids, uuid.New())
		userIDs = append(userIDs, u.id)
		recipients = append(recipients, recipient)
		subjects = append(subjects, subject)
		bodies = append(bodies, body)
		if len(ids) == insertBatch {
			if err := flush(); err != nil {
				return 0, err
			}
		}
	}
	if err := flush(); err != nil {
		return 0, err
	}
	return inserted, nil
}
