package mockprovider

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTest(cfg Config) (*Server, *clock) {
	c := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cfg.Now = c.now
	cfg.Rand = rand.New(rand.NewSource(1))
	return New(cfg), c
}

func do(s *Server, method, path, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

const smsBody = `{"to":"+905551112233","body":"hi"}`

type stats struct {
	Accepted             int            `json:"accepted"`
	Delivered            int            `json:"delivered"`
	DuplicatesSuppressed int            `json:"duplicates_suppressed"`
	Max                  int            `json:"max_deliveries_per_recipient_message"`
	ByChannel            map[string]int `json:"by_channel"`
}

func getStats(t *testing.T, s *Server) stats {
	t.Helper()
	rec := do(s, "GET", "/v1/stats", "", "")
	var st stats
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestMissingIdempotencyKey400(t *testing.T) {
	s, _ := newTest(Config{Idempotency: true})
	if rec := do(s, "POST", "/v1/sms", "", smsBody); rec.Code != 400 {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestIdempotentDuplicateNotRedelivered(t *testing.T) {
	s, _ := newTest(Config{Idempotency: true})
	r1 := do(s, "POST", "/v1/sms", "k1", smsBody)
	r2 := do(s, "POST", "/v1/sms", "k1", smsBody)
	if r1.Code != 202 || r2.Code != 200 {
		t.Fatalf("codes %d %d", r1.Code, r2.Code)
	}
	var a, b map[string]any
	json.Unmarshal(r1.Body.Bytes(), &a)
	json.Unmarshal(r2.Body.Bytes(), &b)
	if b["duplicate"] != true || a["id"] != b["id"] || a["status"] != "accepted" {
		t.Fatalf("bad bodies %v %v", a, b)
	}
	st := getStats(t, s)
	if st.Delivered != 1 || st.DuplicatesSuppressed != 1 || st.Max != 1 || st.ByChannel["sms"] != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestNonIdempotentModeDeliversTwice(t *testing.T) {
	s, _ := newTest(Config{Idempotency: false})
	do(s, "POST", "/v1/sms", "k1", smsBody)
	do(s, "POST", "/v1/sms", "k1", smsBody)
	if st := getStats(t, s); st.Max != 2 || st.Delivered != 2 {
		t.Fatalf("%+v", st)
	}
}

func TestLostResponseStoresThenErrors(t *testing.T) {
	s, _ := newTest(Config{Idempotency: true, LostResponseRate: 1})
	if rec := do(s, "POST", "/v1/sms", "k1", smsBody); rec.Code != 502 {
		t.Fatalf("got %d", rec.Code)
	}
	s.SetConfig(Config{Idempotency: true, Now: s.cfg.Now, Rand: s.cfg.Rand})
	rec := do(s, "POST", "/v1/sms", "k1", smsBody)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"duplicate":true`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if st := getStats(t, s); st.Delivered != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestFailureDoesNotStore(t *testing.T) {
	s, _ := newTest(Config{Idempotency: true, FailRate: 1})
	if rec := do(s, "POST", "/v1/sms", "k1", smsBody); rec.Code != 503 {
		t.Fatalf("got %d", rec.Code)
	}
	if st := getStats(t, s); st.Delivered != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestRateLimit(t *testing.T) {
	s, c := newTest(Config{Idempotency: true, RatePerSecond: 100})
	for i := 0; i < 100; i++ {
		if rec := do(s, "POST", "/v1/sms", fmt.Sprint("k", i), smsBody); rec.Code != 202 {
			t.Fatalf("req %d got %d", i, rec.Code)
		}
	}
	rec := do(s, "POST", "/v1/sms", "over", smsBody)
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("got %d %v", rec.Code, rec.Header())
	}
	if rec := do(s, "POST", "/v1/email", "e1", `{"to":"a@b","subject":"s","body":"b"}`); rec.Code != 202 {
		t.Fatalf("email got %d", rec.Code)
	}
	c.t = c.t.Add(time.Second)
	if rec := do(s, "POST", "/v1/sms", "again", smsBody); rec.Code != 202 {
		t.Fatalf("after window got %d", rec.Code)
	}
	if n := len(s.AcceptedTimes("sms")); n != 101 {
		t.Fatalf("accepted times %d", n)
	}
}

func TestGetMessage(t *testing.T) {
	s, _ := newTest(Config{Idempotency: true})
	do(s, "POST", "/v1/sms", "k1", smsBody)
	rec := do(s, "GET", "/v1/messages/k1", "", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"delivered"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if rec := do(s, "GET", "/v1/messages/nope", "", ""); rec.Code != 404 {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestTimeoutStoresThen504(t *testing.T) {
	s, _ := newTest(Config{Idempotency: true, TimeoutRate: 1, TimeoutDelay: time.Millisecond})
	if rec := do(s, "POST", "/v1/sms", "k1", smsBody); rec.Code != 504 {
		t.Fatalf("got %d", rec.Code)
	}
	if st := getStats(t, s); st.Delivered != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestResetAndHealth(t *testing.T) {
	s, _ := newTest(Config{Idempotency: true})
	do(s, "POST", "/v1/sms", "k1", smsBody)
	if rec := do(s, "POST", "/v1/reset", "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("got %d", rec.Code)
	}
	if st := getStats(t, s); st.Delivered != 0 || len(s.AcceptedTimes("sms")) != 0 {
		t.Fatalf("%+v", st)
	}
	if rec := do(s, "GET", "/healthz", "", ""); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
}
