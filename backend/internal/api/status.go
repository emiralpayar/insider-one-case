package api

import (
	"net/http"
	"strconv"
	"strings"

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

// listMessages is the per-recipient view of a campaign:
// GET /messages?status=failed,unknown&channel=sms&page=2. status may be
// comma-separated and/or repeated.
func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	id, err := campaignID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	var statuses []string
	for _, v := range q["status"] {
		for _, st := range strings.Split(v, ",") {
			if st = strings.TrimSpace(st); st != "" {
				statuses = append(statuses, st)
			}
		}
	}
	page, err := pageParam(q.Get("page"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	res, err := campaign.Messages(r.Context(), s.pool, id, campaign.MessageQuery{
		Statuses: statuses, Channel: q.Get("channel"), Page: page,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// pageParam parses an optional 1-based page number.
func pageParam(v string) (int, error) {
	if v == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, badRequest("page pozitif bir tam sayı olmalı")
	}
	return n, nil
}
