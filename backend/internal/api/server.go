// Package api is the HTTP API of the composer: campaigns, segments, the SSE
// content stream, approval, live status, health and metrics.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/llm"
	"github.com/emiralpayar/campaign-composer/internal/metrics"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Models names the configured model per purpose (reported by /api/config).
type Models struct {
	Segment string `json:"segment"`
	Content string `json:"content"`
}

// Config wires the server.
type Config struct {
	Pool *pgxpool.Pool
	// Composer is nil when no LLM is configured (anthropic without a key):
	// LLM endpoints then answer 503 llm_not_configured.
	Composer    *llm.Composer
	LLMProvider string // "anthropic" | "mock"; recorded on new campaigns
	Models      Models
	// Registry receives the API metrics; a fresh registry is used when nil.
	Registry *prometheus.Registry
	// Now is the clock used as asOf for audience queries (default time.Now).
	Now func() time.Time
	// Heartbeat is the SSE keep-alive comment interval (default 15s).
	Heartbeat time.Duration
}

// Server is the HTTP handler.
type Server struct {
	pool      *pgxpool.Pool
	composer  *llm.Composer
	store     *campaign.Store
	provider  string
	models    Models
	now       func() time.Time
	heartbeat time.Duration
	router    chi.Router
}

// New builds the router.
func New(cfg Config) *Server {
	reg := cfg.Registry
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	m := metrics.NewAPI(reg, queueDepth(cfg.Pool))
	s := &Server{
		pool:      cfg.Pool,
		store:     campaign.NewStore(cfg.Pool, cfg.LLMProvider),
		provider:  cfg.LLMProvider,
		models:    cfg.Models,
		now:       cfg.Now,
		heartbeat: cfg.Heartbeat,
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.heartbeat <= 0 {
		s.heartbeat = 15 * time.Second
	}
	if cfg.Composer != nil {
		// Copy so that the caller's Composer is not mutated; every model call
		// is counted through the call logger.
		c := *cfg.Composer
		c.Log = metricsLogger{next: c.Log, m: m}
		s.composer = &c
	}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)
	r.Method(http.MethodGet, "/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError}))

	r.Route("/api", func(r chi.Router) {
		r.Get("/config", s.config)
		r.Post("/segments/preview", s.previewSegment)
		r.Post("/segments/members", s.segmentMembers)
		r.Post("/campaigns", s.createCampaign)
		r.Get("/campaigns", s.listCampaigns)
		r.Route("/campaigns/{id}", func(r chi.Router) {
			r.Get("/", s.getCampaign)
			r.Put("/name", s.putName)
			r.Post("/segment/generate", s.generateSegment)
			r.Post("/segment/refine", s.refineSegment)
			r.Put("/segment", s.putSegment)
			r.Get("/content/stream", s.contentStream)
			r.Put("/content", s.putContent)
			r.Put("/channels", s.putChannels)
			r.Get("/duplicates", s.duplicates)
			r.Post("/approve", s.approve)
			r.Get("/status", s.status)
			r.Get("/messages", s.listMessages)
		})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]*apiError{"error": {Code: "not_found", Message: "Bulunamadı"}})
	})
	s.router = r
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

// campaignID parses the {id} path parameter; a malformed id is a 404.
func campaignID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, errNotFound
	}
	return id, nil
}

// requireLLM writes 503 and returns false when no LLM is configured.
func (s *Server) requireLLM(w http.ResponseWriter, r *http.Request) bool {
	if s.composer == nil {
		writeError(w, r, errNoLLM)
		return false
	}
	return true
}

// llmFailure maps an error from a Composer call. Errors without a specific
// mapping (transport, timeout) become 502 llm_error.
func llmFailure(err error) error {
	if ae := toAPIError(err); ae.Code != "internal" {
		return err
	}
	slog.Error("llm call failed", "err", err)
	return &apiError{Status: http.StatusBadGateway, Code: "llm_error", Message: "Model çağrısı başarısız oldu; tekrar deneyin"}
}

// metricsLogger counts every model call, then forwards to the real logger.
type metricsLogger struct {
	next llm.CallLogger
	m    *metrics.API
}

func (l metricsLogger) Log(ctx context.Context, rec llm.CallRecord) error {
	result := metrics.LLMResultOK
	switch {
	case rec.Err != "":
		result = metrics.LLMResultError
	case len(rec.ValidationErrors) > 0:
		result = metrics.LLMResultInvalid
	}
	l.m.LLMCall(rec.Purpose, result, rec.Attempt, rec.Latency)
	if l.next == nil {
		return nil
	}
	return l.next.Log(ctx, rec)
}

// queueDepth reads message counts per (channel, status) for /metrics.
func queueDepth(pool *pgxpool.Pool) metrics.QueueDepthFunc {
	if pool == nil {
		return nil
	}
	return func(ctx context.Context) ([]metrics.QueueDepthRow, error) {
		rows, err := pool.Query(ctx, `select channel, status, count(*) from messages group by channel, status order by 1, 2`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []metrics.QueueDepthRow
		for rows.Next() {
			var r metrics.QueueDepthRow
			if err := rows.Scan(&r.Channel, &r.Status, &r.Count); err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, rows.Err()
	}
}
