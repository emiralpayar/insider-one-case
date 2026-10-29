package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

const lease = 30 * time.Second

func TestClaimSkipLockedNoOverlap(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	insertMessages(t, pool, camp, "sms", 200)
	st := NewStore(pool)

	var wg sync.WaitGroup
	results := make([][]Job, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = st.Claim(context.Background(), "sms", 150, lease)
		}()
	}
	wg.Wait()
	seen := map[uuid.UUID]bool{}
	for i := range 2 {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		for _, j := range results[i] {
			if seen[j.ID] {
				t.Fatalf("job %s claimed twice", j.ID)
			}
			seen[j.ID] = true
			if j.Attempts != 1 || j.Recovered || j.Channel != "sms" || j.Recipient == "" || j.Body == "" {
				t.Fatalf("unexpected job %+v", j)
			}
		}
	}
	if len(seen) != 200 {
		t.Fatalf("claimed %d distinct, want 200", len(seen))
	}
	// Nothing left to claim, and nothing for the other channel.
	if more, _ := st.Claim(context.Background(), "sms", 10, lease); len(more) != 0 {
		t.Fatalf("claimed %d more", len(more))
	}
}

func TestClaimRecoversExpiredLease(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 2)
	ctx := context.Background()
	// ids[0]: crashed mid-send (lease expired); ids[1]: in flight elsewhere (lease valid).
	pool.Exec(ctx, `update messages set status='sending', attempts=1, locked_until=now()-interval '1 second' where id=$1`, ids[0])
	pool.Exec(ctx, `update messages set status='sending', attempts=1, locked_until=now()+interval '30 seconds' where id=$1`, ids[1])

	jobs, err := NewStore(pool).Claim(ctx, "sms", 10, lease)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != ids[0] {
		t.Fatalf("got %+v", jobs)
	}
	if !jobs[0].Recovered || jobs[0].Attempts != 2 {
		t.Fatalf("want recovered with attempts 2, got %+v", jobs[0])
	}
}

func TestClaimReturnsCurrentConsent(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 2)
	ctx := context.Background()
	pool.Exec(ctx, `update users set sms_consent=true where id in (select user_id from messages where campaign_id=$1)`, camp)
	pool.Exec(ctx, `update users set sms_consent=false where id=(select user_id from messages where id=$1)`, ids[0])

	jobs, err := NewStore(pool).Claim(ctx, "sms", 10, lease)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]bool{}
	for _, j := range jobs {
		got[j.ID] = j.HasConsent
	}
	if len(got) != 2 || got[ids[0]] || !got[ids[1]] {
		t.Fatalf("consent flags %v", got)
	}
}

func TestMarkRetryRefund(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 1)
	st := NewStore(pool)
	ctx := context.Background()
	jobs, err := st.Claim(ctx, "sms", 1, lease)
	if err != nil || len(jobs) != 1 || jobs[0].Attempts != 1 {
		t.Fatalf("claim: %v %+v", err, jobs)
	}
	if err := st.MarkRetry(ctx, ids[0], time.Now().Add(time.Hour), true, "429"); err != nil {
		t.Fatal(err)
	}
	r := getMessage(t, pool, ids[0])
	if r.Status != "queued" || r.Attempts != 0 || r.LastErr == nil || *r.LastErr != "429" {
		t.Fatalf("got %+v", r)
	}
	// next_attempt_at in the future: not claimable yet.
	if more, _ := st.Claim(ctx, "sms", 1, lease); len(more) != 0 {
		t.Fatal("claimed a message scheduled in the future")
	}
}

func TestMarkTerminalStates(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 4)
	st := NewStore(pool)
	ctx := context.Background()
	if _, err := st.Claim(ctx, "sms", 4, lease); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.MarkSent(ctx, ids[0], "msg-1"))
	must(st.MarkFailed(ctx, ids[1], "400"))
	must(st.MarkUnknown(ctx, ids[2], "503"))
	must(st.MarkSkipped(ctx, ids[3]))
	want := []string{"sent", "failed", "unknown", "skipped_no_consent"}
	for i, id := range ids {
		if s := getMessage(t, pool, id).Status; s != want[i] {
			t.Errorf("%d: %s want %s", i, s, want[i])
		}
	}
	var pid string
	var sentAt *time.Time
	pool.QueryRow(ctx, `select provider_message_id, sent_at from messages where id=$1`, ids[0]).Scan(&pid, &sentAt)
	if pid != "msg-1" || sentAt == nil {
		t.Fatalf("provider id %q sent_at %v", pid, sentAt)
	}
}
