package queue

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// replayServer accepts the first request normally (leaving a keep-alive
// connection in the client's pool). Every later request is read in full —
// i.e. "delivered" — and then the connection is killed without a response.
type replayServer struct {
	mu    sync.Mutex
	addrs []string
}

func (s *replayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	s.mu.Lock()
	s.addrs = append(s.addrs, r.RemoteAddr)
	n := len(s.addrs)
	s.mu.Unlock()
	if n == 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"id":"m1","status":"accepted"}`))
		return
	}
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		conn.Close()
	}
}

func (s *replayServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.addrs...)
}

func sendTwiceOnReusedConn(t *testing.T, idem bool) (*replayServer, SendResult) {
	t.Helper()
	rs := &replayServer{}
	srv := httptest.NewServer(rs)
	t.Cleanup(srv.Close)
	p := NewHTTPProvider(srv.URL, idem, 5*time.Second)
	ctx := context.Background()
	if r := p.Send(ctx, OutboundMessage{ID: uuid.New(), Channel: "sms", To: "+905551112233", Body: "a"}); r.Outcome != OutcomeSent {
		t.Fatalf("warm-up: %+v", r)
	}
	time.Sleep(50 * time.Millisecond) // let the connection return to the idle pool
	res := p.Send(ctx, OutboundMessage{ID: uuid.New(), Channel: "sms", To: "+905551112233", Body: "b"})
	if seen := rs.seen(); len(seen) < 2 || seen[1] != seen[0] {
		t.Fatalf("second request did not reuse the keep-alive connection: %v", seen)
	}
	return rs, res
}

// net/http transparently replays a request with an Idempotency-Key header on
// a reused connection that dies after the request was written. Without
// provider-side dedup that is a hidden double send; the non-idempotent
// provider must surface it as Uncertain after exactly one delivery.
func TestNonIdempotentRequestIsNotReplayed(t *testing.T) {
	rs, res := sendTwiceOnReusedConn(t, false)
	if n := len(rs.seen()); n != 2 {
		t.Fatalf("server received %d requests, want 2 (warm-up + one, no replay)", n)
	}
	if res.Outcome != OutcomeUncertain {
		t.Fatalf("outcome %v (%s), want uncertain", res.Outcome, res.Detail)
	}
}

// Control: with an idempotent provider the replay is allowed (and harmless,
// the provider dedups) — this proves the scenario above really triggers it.
func TestIdempotentRequestMayBeReplayed(t *testing.T) {
	rs, _ := sendTwiceOnReusedConn(t, true)
	if n := len(rs.seen()); n != 3 {
		t.Fatalf("server received %d requests, want 3 (transport replay)", n)
	}
}
