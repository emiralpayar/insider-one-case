package campaign

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/verr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MessageStatuses are the statuses a message row can have (queue package).
var MessageStatuses = []string{"queued", "sending", "sent", "failed", "unknown", "skipped_no_consent"}

// MessagesPerPage is the default page size of the per-recipient list.
const MessagesPerPage = 50

// MaxPage bounds the page number so (Page-1)*PerPage cannot overflow.
const MaxPage = 100000

// MessageQuery filters and pages a campaign's messages. Empty Statuses or
// Channel means "all"; Page starts at 1.
type MessageQuery struct {
	Statuses []string
	Channel  string
	Page     int
	PerPage  int
}

// MessageRow is one message with its recipient's name.
type MessageRow struct {
	ID        uuid.UUID  `json:"id"`
	UserID    int64      `json:"user_id"`
	FirstName string     `json:"first_name"`
	LastName  string     `json:"last_name"`
	Channel   string     `json:"channel"`
	Recipient string     `json:"recipient"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError *string    `json:"last_error"`
	SentAt    *time.Time `json:"sent_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// MessagePage is a page of messages plus the number of matching messages.
type MessagePage struct {
	Messages []MessageRow `json:"messages"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PerPage  int          `json:"per_page"`
}

// Normalize validates the filters and fills page defaults (page 1, 50 per
// page, at most 200).
func (q MessageQuery) Normalize() (MessageQuery, verr.List) {
	var errs verr.List
	allowed := map[string]bool{}
	for _, s := range MessageStatuses {
		allowed[s] = true
	}
	seen := map[string]bool{}
	statuses := []string{}
	for i, s := range q.Statuses {
		if !allowed[s] {
			errs = append(errs, verr.Error{Path: fmt.Sprintf("status[%d]", i), Code: "unknown_status",
				Message: fmt.Sprintf("Bilinmeyen durum: %q", s)})
			continue
		}
		if !seen[s] {
			seen[s] = true
			statuses = append(statuses, s)
		}
	}
	if q.Channel != "" && q.Channel != "sms" && q.Channel != "email" {
		errs = append(errs, verr.Error{Path: "channel", Code: "unknown_channel",
			Message: fmt.Sprintf("Bilinmeyen kanal: %q (izin verilenler: sms, email)", q.Channel)})
	}
	if errs != nil {
		return q, errs
	}
	q.Statuses = statuses
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Page > MaxPage { // (Page-1)*PerPage must not overflow the OFFSET
		return q, verr.List{{Path: "page", Code: "out_of_range", Message: fmt.Sprintf("Sayfa en fazla %d olabilir", MaxPage)}}
	}
	if q.PerPage < 1 || q.PerPage > 200 {
		q.PerPage = MessagesPerPage
	}
	return q, nil
}

// Messages lists a campaign's messages (recipient name, channel, status,
// attempts, last error, sent time), ordered by recipient then channel.
func Messages(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, q MessageQuery) (MessagePage, error) {
	q, errs := q.Normalize()
	if errs != nil {
		return MessagePage{}, errs
	}
	page := MessagePage{Messages: []MessageRow{}, Page: q.Page, PerPage: q.PerPage}
	var exists bool
	if err := pool.QueryRow(ctx, `select true from campaigns where id=$1`, id).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return page, ErrNotFound
		}
		return page, err
	}
	const filter = `m.campaign_id=$1
		and (cardinality($2::text[]) = 0 or m.status = any($2::text[]))
		and ($3::text = '' or m.channel = $3::text)`
	if err := pool.QueryRow(ctx, `select count(*) from messages m where `+filter,
		id, q.Statuses, q.Channel).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := pool.Query(ctx, `select m.id, m.user_id, u.first_name, coalesce(u.last_name, ''), m.channel, m.recipient,
			m.status, m.attempts, m.last_error, m.sent_at, m.updated_at
		from messages m join users u on u.id = m.user_id
		where `+filter+`
		order by m.user_id, m.channel
		limit $4 offset $5`,
		id, q.Statuses, q.Channel, q.PerPage, (q.Page-1)*q.PerPage)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var m MessageRow
		if err := rows.Scan(&m.ID, &m.UserID, &m.FirstName, &m.LastName, &m.Channel, &m.Recipient,
			&m.Status, &m.Attempts, &m.LastError, &m.SentAt, &m.UpdatedAt); err != nil {
			return page, err
		}
		page.Messages = append(page.Messages, m)
	}
	return page, rows.Err()
}
