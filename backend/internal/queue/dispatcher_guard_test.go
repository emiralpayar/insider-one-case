package queue

import (
	"context"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/mockprovider"
)

// Graceful stop while claimed jobs wait for the limiter: they are handed back
// (queued, attempt refunded), not left in 'sending' to be recovered later as
// possibly delivered.
func TestStopReleasesClaimedJobs(t *testing.T) {
	pool := newDB(t, 50)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 5)
	mustExec(t, pool, `update users set sms_consent=true`)
	_, srv := newMock(t, mockprovider.Config{Idempotency: true})
	cp := newCounting(NewHTTPProvider(srv.URL, true, 5*time.Second))
	d := newDispatcher(pool, cp, "sms")
	d.Limiter.Pause(time.Minute) // claims happen, sends wait

	stop := runAll(t, d)
	waitFor(t, 10*time.Second, "jobs claimed", func() bool { return statusCounts(t, pool, camp)["sending"] == 5 })
	stop()
	for _, id := range ids {
		if r := getMessage(t, pool, id); r.Status != "queued" || r.Attempts != 0 {
			t.Fatalf("after stop: %+v", r)
		}
	}
	if cp.Total() != 0 {
		t.Fatalf("provider called %d times", cp.Total())
	}
}

// A job whose lease would run out during the request is not sent (another
// worker could reclaim it meanwhile); it goes back to the queue.
func TestNoSendWithLeaseNearlyExpired(t *testing.T) {
	pool := newDB(t, 50)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 1)
	mustExec(t, pool, `update users set sms_consent=true`)
	_, srv := newMock(t, mockprovider.Config{Idempotency: true})
	cp := newCounting(NewHTTPProvider(srv.URL, true, 5*time.Second))
	d := newDispatcher(pool, cp, "sms")
	d.applyDefaults()

	jobs, err := d.Store.Claim(context.Background(), "sms", 1, 5*time.Second) // < minLeaseLeft
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: %v %d", err, len(jobs))
	}
	d.process(context.Background(), jobs[0])
	if cp.Total() != 0 {
		t.Fatalf("sent with %v of lease left", minLeaseLeft)
	}
	if r := getMessage(t, pool, ids[0]); r.Status != "queued" || r.Attempts != 0 {
		t.Fatalf("got %+v", r)
	}
}

// A recovered row whose attempts are used up ends unknown without another
// send, even with an idempotent provider.
func TestRecoveredExhaustedIsUnknown(t *testing.T) {
	pool := newDB(t, 50)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 1)
	mustExec(t, pool, `update users set sms_consent=true`)
	mustExec(t, pool, `update messages set status='sending', attempts=5, locked_until=now()-interval '1 second' where id=$1`, ids[0])
	_, srv := newMock(t, mockprovider.Config{Idempotency: true})
	cp := newCounting(NewHTTPProvider(srv.URL, true, 5*time.Second))

	stop := runAll(t, newDispatcher(pool, cp, "sms"))
	waitFor(t, 20*time.Second, "terminal", allTerminal(t, pool, camp))
	stop()
	if r := getMessage(t, pool, ids[0]); r.Status != "unknown" {
		t.Fatalf("got %+v", r)
	}
	if cp.Total() != 0 {
		t.Fatalf("provider called %d times", cp.Total())
	}
}
