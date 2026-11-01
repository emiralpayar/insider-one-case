package campaign

import (
	"context"
	"errors"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DuplicateWindow is how far back an identical earlier send counts as a
// duplicate (DECISIONS #27).
const DuplicateWindow = 30 * 24 * time.Hour

// ErrDuplicateNotConfirmed means people in the audience already got the same
// content from another campaign within DuplicateWindow and the request did
// not confirm sending it again.
var ErrDuplicateNotConfirmed = errors.New("campaign: duplicate send not confirmed")

// DuplicateSend is an earlier campaign that sent the same text on a channel
// to some of this campaign's audience.
type DuplicateSend struct {
	CampaignID uuid.UUID `json:"campaign_id"`
	Name       string    `json:"name"`
	Channel    string    `json:"channel"`
	Recipients int       `json:"recipients"` // audience members who got it
	ApprovedAt time.Time `json:"approved_at"`
	// SameText: the channel's text is identical; otherwise only the brief
	// matched (Claude rarely writes the same text twice for one brief).
	SameText bool `json:"same_text"`
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// duplicateSQL finds campaigns approved within the window that had the same
// brief (whitespace and case ignored) or the identical text on an enabled
// channel (SMS text; email subject and body), and counts this audience's
// users with a message from them that was or may have been delivered.
const duplicateSQL = `
with prev as (
  select c.id, c.name, c.approved_at, m.channel, m.user_id,
         case m.channel when 'sms' then c.content->>'sms' = $5
              else c.content->'email'->>'subject' = $7 and c.content->'email'->>'body' = $8 end as same_text,
         lower(regexp_replace(btrim(c.brief), '\s+', ' ', 'g')) = lower(regexp_replace(btrim($9), '\s+', ' ', 'g')) as same_brief
  from messages m join campaigns c on c.id = m.campaign_id
  where c.id <> $1
    and c.approved_at > now() - make_interval(secs => $2)
    and m.user_id = any($3::bigint[])
    and m.status in ('queued','sending','sent','unknown')
    and ((m.channel = 'sms' and $4) or (m.channel = 'email' and $6))
)
select id, name, channel, count(distinct user_id)::int, approved_at, bool_or(same_text)
from prev
where same_text or same_brief
group by id, name, channel, approved_at
order by approved_at desc, channel`

func duplicateSends(ctx context.Context, q queryer, c Campaign, userIDs []int64) ([]DuplicateSend, error) {
	out := []DuplicateSend{}
	if c.Content == nil || len(userIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, duplicateSQL, c.ID, DuplicateWindow.Seconds(), userIDs,
		hasCh(c.Channels, "sms"), c.Content.SMS,
		hasCh(c.Channels, "email"), c.Content.Email.Subject, c.Content.Email.Body, c.Brief)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d DuplicateSend
		if err := rows.Scan(&d.CampaignID, &d.Name, &d.Channel, &d.Recipients, &d.ApprovedAt, &d.SameText); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DuplicateSends reports earlier campaigns that already sent this campaign's
// text to people in its current audience. Approve runs the same check and
// refuses without ConfirmDuplicate.
func DuplicateSends(ctx context.Context, pool *pgxpool.Pool, c Campaign) ([]DuplicateSend, error) {
	if c.Rule == nil {
		return []DuplicateSend{}, nil
	}
	ids, err := segment.MatchIDs(ctx, pool, *c.Rule, time.Now())
	if err != nil {
		return nil, err
	}
	return duplicateSends(ctx, pool, c, ids)
}

func hasCh(chs []string, ch string) bool {
	for _, c := range chs {
		if c == ch {
			return true
		}
	}
	return false
}
