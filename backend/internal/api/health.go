package api

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// llmState is "ok", "mock" or "not_configured".
func (s *Server) llmState() string {
	switch {
	case s.composer == nil:
		return "not_configured"
	case s.provider == "mock":
		return "mock"
	default:
		return "ok"
	}
}

// readyz is 200 when the database answers; the LLM state is informational
// (the API stays usable without a key, only LLM endpoints return 503).
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	body := map[string]string{"db": "ok", "llm": s.llmState()}
	if err := s.pool.Ping(ctx); err != nil {
		body["db"] = "error: " + err.Error()
		writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

type configResponse struct {
	LLMProvider   string `json:"llm_provider"`
	LLMConfigured bool   `json:"llm_configured"`
	Models        Models `json:"models"`
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, configResponse{
		LLMProvider: s.provider, LLMConfigured: s.composer != nil, Models: s.models,
	})
}
