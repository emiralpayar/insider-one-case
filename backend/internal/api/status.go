package api

import (
	"net/http"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
)

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	id, err := campaignID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	rep, err := campaign.Status(r.Context(), s.pool, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}
