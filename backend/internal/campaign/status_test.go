package campaign_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/queue"
	"github.com/google/uuid"
)

func TestStatusReport(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	id := readyCampaign(t, pool, istanbulAbandoned())
	res, err := campaign.Approve(ctx, pool, id, campaign.ApproveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate worker progress: 2 sms sent just now, 1 failed with an error.
	if _, err := pool.Exec(ctx, `update messages set status='sent', sent_at=now()
		where id in (select id from messages where campaign_id=$1 and channel='sms' order by user_id limit 2)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update messages set status='failed', last_error='provider 400: bad number', attempts=1
		where id in (select id from messages where campaign_id=$1 and channel='sms' and status='queued' order by user_id limit 1)`, id); err != nil {
		t.Fatal(err)
	}

	rep, err := campaign.Status(ctx, pool, id)
	if err != nil {
		t.Fatal(err)
	}
	if rep.CampaignStatus != "sending" {
		t.Fatalf("status = %q", rep.CampaignStatus)
	}
	sms := rep.Channels["sms"]
	if sms.Total != res.Enqueued["sms"] || sms.ByStatus["sent"] != 2 || sms.ByStatus["failed"] != 1 ||
		sms.ByStatus["queued"] != res.Enqueued["sms"]-3 {
		t.Fatalf("sms = %+v (enqueued %d)", sms, res.Enqueued["sms"])
	}
	if sms.SentLast10s != 2 || sms.RatePerSecond != 0.2 {
		t.Fatalf("rate: %+v", sms)
	}
	if rep.Channels["email"].Total != res.Enqueued["email"] {
		t.Fatalf("email = %+v", rep.Channels["email"])
	}
	if len(rep.RecentErrors) != 1 || rep.RecentErrors[0].Error != "provider 400: bad number" || rep.RecentErrors[0].Channel != "sms" {
		t.Fatalf("recent errors = %+v", rep.RecentErrors)
	}

	if _, err := campaign.Status(ctx, pool, uuid.New()); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestStatusDraftHasZeroChannels(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	c, err := campaign.NewStore(pool, "mock").Create(ctx, "brief")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := campaign.Status(ctx, pool, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep.CampaignStatus != "draft" || len(rep.Channels) != 2 || rep.Channels["sms"].Total != 0 || rep.RecentErrors == nil {
		t.Fatalf("rep = %+v", rep)
	}
}

// A message that failed once and was then sent on retry keeps its last_error,
// so the dashboard's "recent errors" shows the retry history with the
// message's current status (sent) instead of hiding it.
func TestStatusKeepsErrorOfRetriedThenSentMessage(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	id := readyCampaign(t, pool, istanbulAbandoned())
	if _, err := campaign.Approve(ctx, pool, id, campaign.ApproveRequest{}); err != nil {
		t.Fatal(err)
	}
	st := queue.NewStore(pool)
	jobs, err := st.Claim(ctx, "sms", 1, 30*time.Second)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: %v %d", err, len(jobs))
	}
	if err := st.MarkRetry(ctx, jobs[0], time.Now().Add(-time.Second), false, "provider 503: unavailable"); err != nil {
		t.Fatal(err)
	}
	again, err := st.Claim(ctx, "sms", 1, 30*time.Second)
	if err != nil || len(again) != 1 || again[0].ID != jobs[0].ID || again[0].Attempts != 2 {
		t.Fatalf("reclaim: %v %+v", err, again)
	}
	if err := st.MarkSent(ctx, again[0], "pm-1"); err != nil {
		t.Fatal(err)
	}

	rep, err := campaign.Status(ctx, pool, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.RecentErrors) != 1 {
		t.Fatalf("recent errors = %+v, want the retried message", rep.RecentErrors)
	}
	e := rep.RecentErrors[0]
	if e.MessageID != jobs[0].ID || e.Status != "sent" || e.Attempts != 2 || e.Error != "provider 503: unavailable" {
		t.Fatalf("recent error = %+v", e)
	}
	if rep.Channels["sms"].ByStatus["sent"] != 1 {
		t.Fatalf("sms = %+v", rep.Channels["sms"])
	}
}
