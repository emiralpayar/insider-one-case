package campaign

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// rateWindow is the window used for the per-channel send rate.
const rateWindow = 10 * time.Second

// recentErrorLimit is the number of recent message errors in a report.
const recentErrorLimit = 10

// StatusReport is the live sending status of a campaign.
type StatusReport struct {
	CampaignStatus string                   `json:"campaign_status"`
	Channels       map[string]ChannelStatus `json:"channels"`
	RecentErrors   []MessageError           `json:"recent_errors"`
}

// ChannelStatus counts a channel's messages.
type ChannelStatus struct {
	Total         int            `json:"total"`
	ByStatus      map[string]int `json:"by_status"`
	SentLast10s   int            `json:"sent_last_10s"`
	RatePerSecond float64        `json:"rate_per_second"`
}

// MessageError is a message that recorded an error.
type MessageError struct {
	MessageID uuid.UUID `json:"message_id"`
	UserID    int64     `json:"user_id"`
	Channel   string    `json:"channel"`
	Status    string    `json:"status"`
	Attempts  int       `json:"attempts"`
	Error     string    `json:"error"`
	At        time.Time `json:"at"`
}

// Status builds the status report; every enabled channel is present even
// before approval.
func Status(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (StatusReport, error) {
	rep := StatusReport{Channels: map[string]ChannelStatus{}, RecentErrors: []MessageError{}}
	var channels []string
	err := pool.QueryRow(ctx, `select status, channels from campaigns where id=$1`, id).Scan(&rep.CampaignStatus, &channels)
	if errors.Is(err, pgx.ErrNoRows) {
		return rep, ErrNotFound
	}
	if err != nil {
		return rep, err
	}
	for _, ch := range channels {
		rep.Channels[ch] = ChannelStatus{ByStatus: map[string]int{}}
	}

	rows, err := pool.Query(ctx, `select channel, status, count(*),
		count(*) filter (where status='sent' and sent_at > now() - make_interval(secs => $2))
		from messages where campaign_id=$1 group by channel, status`, id, rateWindow.Seconds())
	if err != nil {
		return rep, err
	}
	defer rows.Close()
	for rows.Next() {
		var ch, st string
		var n, recent int
		if err := rows.Scan(&ch, &st, &n, &recent); err != nil {
			return rep, err
		}
		cs, ok := rep.Channels[ch]
		if !ok {
			cs = ChannelStatus{ByStatus: map[string]int{}}
		}
		cs.Total += n
		cs.ByStatus[st] = n
		cs.SentLast10s += recent
		cs.RatePerSecond = float64(cs.SentLast10s) / rateWindow.Seconds()
		rep.Channels[ch] = cs
	}
	if err := rows.Err(); err != nil {
		return rep, err
	}

	erows, err := pool.Query(ctx, `select id, user_id, channel, status, attempts, last_error, updated_at
		from messages where campaign_id=$1 and last_error is not null
		order by updated_at desc, id limit $2`, id, recentErrorLimit)
	if err != nil {
		return rep, err
	}
	defer erows.Close()
	for erows.Next() {
		var e MessageError
		if err := erows.Scan(&e.MessageID, &e.UserID, &e.Channel, &e.Status, &e.Attempts, &e.Error, &e.At); err != nil {
			return rep, err
		}
		rep.RecentErrors = append(rep.RecentErrors, e)
	}
	return rep, erows.Err()
}
