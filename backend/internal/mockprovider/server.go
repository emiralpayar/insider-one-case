package mockprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

type record struct {
	ID         string
	Channel    string
	To         string
	Deliveries int
}

type msgKey struct{ channel, to, subject, body string }

// Server is the mock provider HTTP handler.
type Server struct {
	mu   sync.Mutex
	cfg  Config
	mux  *http.ServeMux
	seq  int
	msgs map[string]*record // by idempotency key
	per  map[msgKey]int     // deliveries per (channel,to,subject,body)

	window     map[string][]time.Time // rate-limit window per channel
	accepted   map[string][]time.Time // delivery times per channel
	acceptedN  int
	delivered  int
	duplicates int
}

// New creates a Server. Now and Rand default to wall clock / time-seeded rand.
func New(cfg Config) *Server {
	s := &Server{mux: http.NewServeMux()}
	s.SetConfig(cfg)
	s.resetLocked()
	s.mux.HandleFunc("POST /v1/sms", func(w http.ResponseWriter, r *http.Request) { s.send(w, r, "sms") })
	s.mux.HandleFunc("POST /v1/email", func(w http.ResponseWriter, r *http.Request) { s.send(w, r, "email") })
	s.mux.HandleFunc("GET /v1/messages/{key}", s.getMessage)
	s.mux.HandleFunc("GET /v1/stats", s.stats)
	s.mux.HandleFunc("POST /v1/reset", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		s.resetLocked()
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	return s
}

// SetConfig replaces the configuration (tests / runtime tuning).
func (s *Server) SetConfig(cfg Config) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
}

func (s *Server) resetLocked() {
	s.seq, s.acceptedN, s.delivered, s.duplicates = 0, 0, 0, 0
	s.msgs = map[string]*record{}
	s.per = map[msgKey]int{}
	s.window = map[string][]time.Time{}
	s.accepted = map[string][]time.Time{}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// AcceptedTimes returns the times of new deliveries on a channel (tests only).
func (s *Server) AcceptedTimes(channel string) []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.accepted[channel]...)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

type sendReq struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (s *Server) send(w http.ResponseWriter, r *http.Request, channel string) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeJSON(w, 400, map[string]string{"error": "Idempotency-Key header required"})
		return
	}
	var req sendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.To == "" {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}

	s.mu.Lock()
	cfg := s.cfg
	now := cfg.Now()
	// rate limit gate (rejected requests do not consume the window)
	if cfg.RatePerSecond > 0 {
		cutoff := now.Add(-time.Second)
		win := s.window[channel]
		i := 0
		for i < len(win) && !win[i].After(cutoff) {
			i++
		}
		win = win[i:]
		if len(win) >= cfg.RatePerSecond {
			s.window[channel] = win
			s.mu.Unlock()
			tooMany(w)
			return
		}
		s.window[channel] = append(win, now)
	}
	if cfg.Rand.Float64() < cfg.RateLimited429 {
		s.mu.Unlock()
		tooMany(w)
		return
	}
	if cfg.Rand.Float64() < cfg.FailRate {
		s.mu.Unlock()
		writeJSON(w, 503, map[string]string{"error": "provider unavailable"})
		return
	}
	s.acceptedN++
	if cfg.Idempotency {
		if rec, ok := s.msgs[key]; ok {
			s.duplicates++
			s.mu.Unlock()
			writeJSON(w, 200, map[string]any{"id": rec.ID, "status": "accepted", "duplicate": true})
			return
		}
	}
	// store / deliver
	rec, ok := s.msgs[key]
	if !ok {
		s.seq++
		rec = &record{ID: fmt.Sprintf("msg-%d", s.seq), Channel: channel, To: req.To}
		s.msgs[key] = rec
	}
	rec.Deliveries++
	s.delivered++
	s.per[msgKey{channel, req.To, req.Subject, req.Body}]++
	s.accepted[channel] = append(s.accepted[channel], now)
	id := rec.ID
	lost := cfg.Rand.Float64() < cfg.LostResponseRate
	timeout := !lost && cfg.Rand.Float64() < cfg.TimeoutRate
	var latency time.Duration
	if cfg.LatencyMax > cfg.LatencyMin {
		latency = cfg.LatencyMin + time.Duration(cfg.Rand.Int63n(int64(cfg.LatencyMax-cfg.LatencyMin)))
	} else {
		latency = cfg.LatencyMin
	}
	s.mu.Unlock()

	if !sleep(r.Context(), latency) {
		return
	}
	switch {
	case lost:
		writeJSON(w, 502, map[string]string{"error": "bad gateway"})
	case timeout:
		if !sleep(r.Context(), cfg.TimeoutDelay) {
			return
		}
		writeJSON(w, 504, map[string]string{"error": "gateway timeout"})
	default:
		writeJSON(w, 202, map[string]string{"id": id, "status": "accepted"})
	}
}

func tooMany(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	writeJSON(w, 429, map[string]string{"error": "rate limited"})
}

// sleep waits d or until ctx is done; returns false if ctx was cancelled.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	rec, ok := s.msgs[r.PathValue("key")]
	var out map[string]any
	if ok {
		out = map[string]any{"id": rec.ID, "channel": rec.Channel, "to": rec.To, "status": "delivered", "deliveries": rec.Deliveries}
	}
	s.mu.Unlock()
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) stats(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	max := 0
	for _, n := range s.per {
		if n > max {
			max = n
		}
	}
	by := map[string]int{"sms": len(s.accepted["sms"]), "email": len(s.accepted["email"])}
	out := map[string]any{
		"accepted": s.acceptedN, "delivered": s.delivered, "duplicates_suppressed": s.duplicates,
		"max_deliveries_per_recipient_message": max, "by_channel": by,
	}
	s.mu.Unlock()
	writeJSON(w, 200, out)
}
