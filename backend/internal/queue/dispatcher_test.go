package queue

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/mockprovider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type providerStats struct {
	Accepted  int `json:"accepted"`
	Delivered int `json:"delivered"`
	Dupes     int `json:"duplicates_suppressed"`
	Max       int `json:"max_deliveries_per_recipient_message"`
}

func newMock(t *testing.T, cfg mockprovider.Config) (*mockprovider.Server, *httptest.Server) {
	t.Helper()
	if cfg.LatencyMax == 0 {
		cfg.LatencyMax = 5 * time.Millisecond
	}
	cfg.Rand = rand.New(rand.NewSource(7))
	m := mockprovider.New(cfg)
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	return m, srv
}

func fetchStats(t *testing.T, srv *httptest.Server) providerStats {
	t.Helper()
	resp, err := http.Get(srv.URL + "/v1/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var s providerStats
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func newDispatcher(pool *pgxpool.Pool, p Provider, channel string) *Dispatcher {
	r := rand.New(rand.NewSource(3))
	var mu sync.Mutex
	return &Dispatcher{
		Pool: pool, Store: NewStore(pool), Provider: p, Channel: channel,
		Limiter: NewLimiter(100, time.Now), Concurrency: 20, BatchSize: 100, MaxAttempts: 5,
		Rand: func() float64 {
			mu.Lock()
			defer mu.Unlock()
			return r.Float64()
		},
		BackoffScale: time.Millisecond,
	}
}

// runAll starts the dispatchers and returns a stop func that cancels them and
// waits for Run to return (failing the test if it does not within 15s).
func runAll(t *testing.T, ds ...*Dispatcher) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, d := range ds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.Run(ctx); err != nil {
				t.Errorf("Run: %v", err)
			}
		}()
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Error("dispatchers did not stop within 15s")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func allTerminal(t *testing.T, pool *pgxpool.Pool, camp uuid.UUID) func() bool {
	return func() bool {
		c := statusCounts(t, pool, camp)
		return c["queued"] == 0 && c["sending"] == 0
	}
}

func TestEndToEndExactlyOncePerRecipient(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	insertMessages(t, pool, camp, "sms", 150)
	_, srv := newMock(t, mockprovider.Config{FailRate: 0.1, RateLimited429: 0.05, LostResponseRate: 0.05, Idempotency: true})
	p := NewHTTPProvider(srv.URL, true, 5*time.Second)
	// Ignore consent for this test: everyone in the campaign has it.
	pool.Exec(context.Background(), `update users set sms_consent=true`)

	stop := runAll(t, newDispatcher(pool, p, "sms"), newDispatcher(pool, p, "sms"))
	waitFor(t, 60*time.Second, "all messages terminal", allTerminal(t, pool, camp))
	stop()

	st := fetchStats(t, srv)
	c := statusCounts(t, pool, camp)
	t.Logf("db=%v provider=%+v", c, st)
	if st.Max != 1 {
		t.Fatalf("max deliveries per recipient message = %d", st.Max)
	}
	if c["sent"] != st.Delivered {
		t.Fatalf("sent rows %d != provider delivered %d", c["sent"], st.Delivered)
	}
	if c["sent"] < 140 {
		t.Fatalf("only %d sent", c["sent"])
	}
}

func TestRateLimitAcrossWorkers(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	insertMessages(t, pool, camp, "sms", 300)
	pool.Exec(context.Background(), `update users set sms_consent=true`)
	m, srv := newMock(t, mockprovider.Config{Idempotency: true}) // real clock, no faults, no provider-side limit
	p := NewHTTPProvider(srv.URL, true, 5*time.Second)

	stop := runAll(t, newDispatcher(pool, p, "sms"), newDispatcher(pool, p, "sms"), newDispatcher(pool, p, "sms"))
	waitFor(t, 30*time.Second, "all messages terminal", allTerminal(t, pool, camp))
	stop()

	times := m.AcceptedTimes("sms")
	if len(times) != 300 {
		t.Fatalf("accepted %d, want 300", len(times))
	}
	if mx := maxInWindow(times, time.Second); mx > 100 {
		t.Fatalf("provider saw %d messages within one second (limit 100)", mx)
	}
	if c := statusCounts(t, pool, camp); c["sent"] != 300 {
		t.Fatalf("statuses %v", c)
	}
}

func TestExpiredLockNonIdempotentBecomesUnknown(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 1)
	ctx := context.Background()
	pool.Exec(ctx, `update users set sms_consent=true`)
	pool.Exec(ctx, `update messages set status='sending', attempts=1, locked_until=now()-interval '1 second' where id=$1`, ids[0])
	_, srv := newMock(t, mockprovider.Config{Idempotency: false})
	cp := newCounting(NewHTTPProvider(srv.URL, false, 5*time.Second))

	runAll(t, newDispatcher(pool, cp, "sms"))
	waitFor(t, 20*time.Second, "terminal", allTerminal(t, pool, camp))
	if r := getMessage(t, pool, ids[0]); r.Status != "unknown" {
		t.Fatalf("got %+v", r)
	}
	if cp.Total() != 0 {
		t.Fatalf("provider called %d times", cp.Total())
	}
}

func TestExpiredLockIdempotentResends(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 1)
	ctx := context.Background()
	pool.Exec(ctx, `update users set sms_consent=true`)
	pool.Exec(ctx, `update messages set status='sending', attempts=1, locked_until=now()-interval '1 second' where id=$1`, ids[0])
	_, srv := newMock(t, mockprovider.Config{Idempotency: true})
	cp := newCounting(NewHTTPProvider(srv.URL, true, 5*time.Second))

	runAll(t, newDispatcher(pool, cp, "sms"))
	waitFor(t, 20*time.Second, "terminal", allTerminal(t, pool, camp))
	if r := getMessage(t, pool, ids[0]); r.Status != "sent" || r.Attempts != 2 {
		t.Fatalf("got %+v", r)
	}
}

func TestRevokedConsentSkipped(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "email", 3)
	ctx := context.Background()
	pool.Exec(ctx, `update users set email_consent=true`)
	// Revoked after approval (message rows already exist).
	pool.Exec(ctx, `update users set email_consent=false where id=(select user_id from messages where id=$1)`, ids[0])
	_, srv := newMock(t, mockprovider.Config{Idempotency: true})
	cp := newCounting(NewHTTPProvider(srv.URL, true, 5*time.Second))

	runAll(t, newDispatcher(pool, cp, "email"))
	waitFor(t, 20*time.Second, "terminal", allTerminal(t, pool, camp))
	if r := getMessage(t, pool, ids[0]); r.Status != "skipped_no_consent" {
		t.Fatalf("got %+v", r)
	}
	cp.mu.Lock()
	n := cp.calls[ids[0]]
	cp.mu.Unlock()
	if n != 0 {
		t.Fatalf("provider called %d times for revoked user", n)
	}
	for _, id := range ids[1:] {
		if r := getMessage(t, pool, id); r.Status != "sent" {
			t.Fatalf("consenting user: %+v", r)
		}
	}
}

func TestRateLimitedDoesNotConsumeAttempts(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	// Each 429 pauses the whole channel for Retry-After (1s), so keep this small.
	ids := insertMessages(t, pool, camp, "sms", 2)
	pool.Exec(context.Background(), `update users set sms_consent=true`)
	_, srv := newMock(t, mockprovider.Config{RateLimited429: 1, Idempotency: true})
	cp := newCounting(NewHTTPProvider(srv.URL, true, 5*time.Second))

	stop := runAll(t, newDispatcher(pool, cp, "sms"))
	waitFor(t, 30*time.Second, "3 rounds of 429", func() bool { return cp.MinCalls(ids) >= 3 })
	stop()
	for _, id := range ids {
		if r := getMessage(t, pool, id); r.Status != "queued" || r.Attempts != 0 {
			t.Fatalf("got %+v", r)
		}
	}
}

func TestUncertainExhaustedIsUnknown(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 3)
	pool.Exec(context.Background(), `update users set sms_consent=true`)
	_, srv := newMock(t, mockprovider.Config{FailRate: 1, Idempotency: true})
	cp := newCounting(NewHTTPProvider(srv.URL, true, 5*time.Second))
	d := newDispatcher(pool, cp, "sms")
	d.MaxAttempts = 2

	runAll(t, d)
	waitFor(t, 20*time.Second, "terminal", allTerminal(t, pool, camp))
	for _, id := range ids {
		if r := getMessage(t, pool, id); r.Status != "unknown" || r.Attempts != 2 {
			t.Fatalf("got %+v", r)
		}
	}
}

func TestNotSentExhaustedIsFailed(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 2)
	pool.Exec(context.Background(), `update users set sms_consent=true`)
	// A closed server: connection refused, the request never left.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	d := newDispatcher(pool, NewHTTPProvider(url, false, 5*time.Second), "sms")
	d.MaxAttempts = 3

	runAll(t, d)
	waitFor(t, 20*time.Second, "terminal", allTerminal(t, pool, camp))
	for _, id := range ids {
		if r := getMessage(t, pool, id); r.Status != "failed" || r.Attempts != 3 {
			t.Fatalf("got %+v", r)
		}
	}
}

func TestPermanentFailure(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 1)
	ctx := context.Background()
	pool.Exec(ctx, `update users set sms_consent=true`)
	pool.Exec(ctx, `update messages set recipient='' where id=$1`, ids[0]) // mock: 400 invalid body
	_, srv := newMock(t, mockprovider.Config{Idempotency: true})

	runAll(t, newDispatcher(pool, NewHTTPProvider(srv.URL, true, 5*time.Second), "sms"))
	waitFor(t, 20*time.Second, "terminal", allTerminal(t, pool, camp))
	if r := getMessage(t, pool, ids[0]); r.Status != "failed" || r.Attempts != 1 {
		t.Fatalf("got %+v", r)
	}
}

func TestLeaderOnlyOneSends(t *testing.T) {
	pool := newDB(t, 300)
	camp := insertCampaign(t, pool, "sending")
	insertMessages(t, pool, camp, "sms", 50)
	pool.Exec(context.Background(), `update users set sms_consent=true`)
	_, srv := newMock(t, mockprovider.Config{Idempotency: true})
	p := NewHTTPProvider(srv.URL, true, 5*time.Second)
	a, b := newCounting(p), newCounting(p)

	runAll(t, newDispatcher(pool, a, "sms"), newDispatcher(pool, b, "sms"))
	waitFor(t, 20*time.Second, "terminal", allTerminal(t, pool, camp))
	if a.Total() != 0 && b.Total() != 0 {
		t.Fatalf("both dispatchers sent: %d and %d", a.Total(), b.Total())
	}
}

func TestCompleteCampaigns(t *testing.T) {
	pool := newDB(t, 300)
	ctx := context.Background()
	done := insertCampaign(t, pool, "sending")
	busy := insertCampaign(t, pool, "sending")
	draft := insertCampaign(t, pool, "draft")
	ids := insertMessages(t, pool, done, "sms", 4)
	insertMessages(t, pool, busy, "sms", 2)
	statuses := []string{"sent", "failed", "unknown", "skipped_no_consent"}
	for i, id := range ids {
		pool.Exec(ctx, `update messages set status=$2 where id=$1`, id, statuses[i])
	}
	pool.Exec(ctx, `update messages set status='sent' where campaign_id=$1 and id=(select min(id::text)::uuid from messages where campaign_id=$1)`, busy)

	n, err := CompleteCampaigns(ctx, pool)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	want := map[uuid.UUID]string{done: "completed", busy: "sending", draft: "draft"}
	for id, w := range want {
		var s string
		pool.QueryRow(ctx, `select status from campaigns where id=$1`, id).Scan(&s)
		if s != w {
			t.Errorf("campaign %s: %s want %s", id, s, w)
		}
	}
	if n, _ := CompleteCampaigns(ctx, pool); n != 0 {
		t.Fatalf("second run completed %d", n)
	}
}
