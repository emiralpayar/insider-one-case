package campaign

import (
	"context"
	"errors"
	"log"
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

// DuplicateSend is an earlier campaign that reached some of this campaign's
// audience on a channel with the same text, the same brief, or (with the
// optional LLM check) a brief the model judged to have the same purpose.
type DuplicateSend struct {
	CampaignID uuid.UUID `json:"campaign_id"`
	Name       string    `json:"name"`
	Channel    string    `json:"channel"`
	Recipients int       `json:"recipients"` // audience members who got it
	ApprovedAt time.Time `json:"approved_at"`
	// Match is "text" (identical text on this channel), "brief" (same brief,
	// whitespace and case ignored; Claude rarely writes the same text twice)
	// or "similar" (LLM check, DECISIONS #29; Reason says why).
	Match  string `json:"match"`
	Reason string `json:"reason,omitempty"`
}

// DuplicateReport is DuplicateSends' result. SimilarityCheck is "off" (not
// enabled), "ok" (ran, or nothing to compare) or "error" (the LLM failed;
// only the exact checks were applied).
type DuplicateReport struct {
	Duplicates      []DuplicateSend `json:"duplicates"`
	SimilarityCheck string          `json:"similarity_check"`
}

// SimilarCandidate is an earlier campaign offered to the LLM check.
type SimilarCandidate struct {
	ID    uuid.UUID
	Brief string
}

// SimilarFunc returns, for the candidates with the same purpose as brief,
// the model's one-sentence reason keyed by campaign id.
type SimilarFunc func(ctx context.Context, brief string, cands []SimilarCandidate) (map[uuid.UUID]string, error)

// maxSimilarCandidates bounds the LLM prompt: the most recent campaigns first.
const maxSimilarCandidates = 10

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// recentSendsSQL lists, per campaign and enabled channel, the campaigns
// approved within the window that sent this audience's users a message that
// was or may have been delivered, with whether the text or brief is the same.
const recentSendsSQL = `
with prev as (
  select c.id, c.name, c.brief, c.approved_at, m.channel, m.user_id,
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
select id, name, brief, channel, count(distinct user_id)::int, approved_at, coalesce(bool_or(same_text), false), bool_or(same_brief)
from prev
group by id, name, brief, channel, approved_at
order by approved_at desc, channel`

type recentSend struct {
	DuplicateSend
	brief               string
	sameText, sameBrief bool
}

func recentSends(ctx context.Context, q queryer, c Campaign, userIDs []int64) ([]recentSend, error) {
	if c.Content == nil || len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, recentSendsSQL, c.ID, DuplicateWindow.Seconds(), userIDs,
		hasCh(c.Channels, "sms"), c.Content.SMS,
		hasCh(c.Channels, "email"), c.Content.Email.Subject, c.Content.Email.Body, c.Brief)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []recentSend
	for rows.Next() {
		var r recentSend
		if err := rows.Scan(&r.CampaignID, &r.Name, &r.brief, &r.Channel, &r.Recipients, &r.ApprovedAt, &r.sameText, &r.sameBrief); err != nil {
			return nil, err
		}
		switch {
		case r.sameText:
			r.Match = "text"
		case r.sameBrief:
			r.Match = "brief"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// duplicateSends is the exact check (same text or same brief). Approve uses
// it inside its transaction; it never calls the LLM.
func duplicateSends(ctx context.Context, q queryer, c Campaign, userIDs []int64) ([]DuplicateSend, error) {
	rs, err := recentSends(ctx, q, c, userIDs)
	if err != nil {
		return nil, err
	}
	out := []DuplicateSend{}
	for _, r := range rs {
		if r.Match != "" {
			out = append(out, r.DuplicateSend)
		}
	}
	return out, nil
}

// DuplicateSends reports earlier campaigns that already reached people in
// this campaign's current audience with the same text or brief. With
// similar != nil, the remaining campaigns of the window that reached the
// same people (at most maxSimilarCandidates, newest first) are also judged
// by the LLM; if that fails, the exact result is returned with
// SimilarityCheck "error". Approve enforces only the exact matches.
func DuplicateSends(ctx context.Context, pool *pgxpool.Pool, c Campaign, similar SimilarFunc) (DuplicateReport, error) {
	rep := DuplicateReport{Duplicates: []DuplicateSend{}, SimilarityCheck: "off"}
	if similar != nil {
		rep.SimilarityCheck = "ok"
	}
	if c.Rule == nil {
		return rep, nil
	}
	ids, err := segment.MatchIDs(ctx, pool, *c.Rule, time.Now())
	if err != nil {
		return rep, err
	}
	rs, err := recentSends(ctx, pool, c, ids)
	if err != nil {
		return rep, err
	}
	var cands []SimilarCandidate
	seen := map[uuid.UUID]bool{}
	for _, r := range rs {
		if r.Match != "" {
			rep.Duplicates = append(rep.Duplicates, r.DuplicateSend)
		} else if !seen[r.CampaignID] && len(cands) < maxSimilarCandidates {
			seen[r.CampaignID] = true
			cands = append(cands, SimilarCandidate{ID: r.CampaignID, Brief: r.brief})
		}
	}
	if similar == nil || len(cands) == 0 {
		return rep, nil
	}
	reasons, err := similar(ctx, c.Brief, cands)
	if err != nil {
		log.Printf("duplicates %s: similarity check failed, using exact checks only: %v", c.ID, err)
		rep.SimilarityCheck = "error"
		return rep, nil
	}
	for _, r := range rs {
		if reason, ok := reasons[r.CampaignID]; ok && r.Match == "" {
			r.Match, r.Reason = "similar", reason
			rep.Duplicates = append(rep.Duplicates, r.DuplicateSend)
		}
	}
	return rep, nil
}

func hasCh(chs []string, ch string) bool {
	for _, c := range chs {
		if c == ch {
			return true
		}
	}
	return false
}
