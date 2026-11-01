package campaign_test

import (
	"context"
	"errors"
	"testing"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/verr"
	"github.com/google/uuid"
)

func TestMessagesFilterAndPage(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	id := readyCampaign(t, pool, istanbulAbandoned())
	res, err := campaign.Approve(ctx, pool, id, campaign.ApproveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	total := res.Enqueued["sms"] + res.Enqueued["email"]
	if res.Enqueued["sms"] < 3 {
		t.Fatalf("need ≥3 sms, got %v", res.Enqueued)
	}
	// 1 sms sent, 1 sms failed, 1 sms unknown; the rest stay queued.
	for _, upd := range []string{
		`update messages set status='sent', sent_at=now(), attempts=1 where id = (select id from messages where campaign_id=$1 and channel='sms' and status='queued' order by user_id limit 1)`,
		`update messages set status='failed', attempts=3, last_error='provider 400: bad number' where id = (select id from messages where campaign_id=$1 and channel='sms' and status='queued' order by user_id limit 1)`,
		`update messages set status='unknown', attempts=1, last_error='timeout' where id = (select id from messages where campaign_id=$1 and channel='sms' and status='queued' order by user_id limit 1)`,
	} {
		if _, err := pool.Exec(ctx, upd, id); err != nil {
			t.Fatal(err)
		}
	}

	all, err := campaign.Messages(ctx, pool, id, campaign.MessageQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != total || all.Page != 1 || all.PerPage != 50 || len(all.Messages) != min(total, 50) {
		t.Fatalf("all: total=%d page=%d per=%d len=%d (want total %d)", all.Total, all.Page, all.PerPage, len(all.Messages), total)
	}
	m0 := all.Messages[0]
	if m0.FirstName == "" && m0.LastName == "" || m0.Recipient == "" {
		t.Fatalf("first row lacks recipient data: %+v", m0)
	}

	// "Gitmeyenler": failed + unknown + skipped_no_consent
	notSent, err := campaign.Messages(ctx, pool, id, campaign.MessageQuery{Statuses: []string{"failed", "unknown", "skipped_no_consent"}})
	if err != nil {
		t.Fatal(err)
	}
	if notSent.Total != 2 || len(notSent.Messages) != 2 {
		t.Fatalf("not sent = %+v", notSent)
	}
	for _, m := range notSent.Messages {
		if m.Channel != "sms" || m.LastError == nil || m.SentAt != nil {
			t.Fatalf("not-sent row = %+v", m)
		}
	}

	sent, err := campaign.Messages(ctx, pool, id, campaign.MessageQuery{Statuses: []string{"sent"}, Channel: "sms"})
	if err != nil || sent.Total != 1 || sent.Messages[0].SentAt == nil || sent.Messages[0].Attempts != 1 {
		t.Fatalf("sent = %+v %v", sent, err)
	}
	email, err := campaign.Messages(ctx, pool, id, campaign.MessageQuery{Channel: "email"})
	if err != nil || email.Total != res.Enqueued["email"] {
		t.Fatalf("email = %d %v (want %d)", email.Total, err, res.Enqueued["email"])
	}

	// paging: pages of 2 cover every message exactly once, in order
	seen := map[uuid.UUID]bool{}
	for p := 1; ; p++ {
		pg, err := campaign.Messages(ctx, pool, id, campaign.MessageQuery{Page: p, PerPage: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(pg.Messages) == 0 {
			break
		}
		for _, m := range pg.Messages {
			if seen[m.ID] {
				t.Fatalf("message %s on two pages", m.ID)
			}
			seen[m.ID] = true
		}
	}
	if len(seen) != total {
		t.Fatalf("paged %d of %d", len(seen), total)
	}
}

func TestMessagesValidation(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	id := readyCampaign(t, pool, istanbulAbandoned())
	var vl verr.List
	if _, err := campaign.Messages(ctx, pool, id, campaign.MessageQuery{Statuses: []string{"sent'; drop table messages; --"}}); !errors.As(err, &vl) || vl[0].Code != "unknown_status" {
		t.Fatalf("bad status: %v", err)
	}
	if _, err := campaign.Messages(ctx, pool, id, campaign.MessageQuery{Channel: "fax"}); !errors.As(err, &vl) || vl[0].Code != "unknown_channel" {
		t.Fatalf("bad channel: %v", err)
	}
	if _, err := campaign.Messages(ctx, pool, uuid.New(), campaign.MessageQuery{}); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// before approval: an empty list, not an error
	pg, err := campaign.Messages(ctx, pool, id, campaign.MessageQuery{})
	if err != nil || pg.Total != 0 || pg.Messages == nil {
		t.Fatalf("draft: %+v %v", pg, err)
	}
}
