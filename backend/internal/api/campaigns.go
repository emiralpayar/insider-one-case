package api

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/content"
	"github.com/emiralpayar/campaign-composer/internal/verr"
)

// maxBriefRunes bounds the brief length.
const maxBriefRunes = 2000

func (s *Server) createCampaign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Brief string `json:"brief"`
	}
	if err := decode(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	req.Brief = strings.TrimSpace(req.Brief)
	switch n := utf8.RuneCountInString(req.Brief); {
	case n == 0:
		writeError(w, r, verr.List{{Path: "brief", Code: "empty_field", Message: "Brief boş olamaz"}})
		return
	case n > maxBriefRunes:
		writeError(w, r, verr.List{{Path: "brief", Code: "too_long", Message: "Brief en fazla 2000 karakter olabilir"}})
		return
	}
	c, err := s.store.Create(r.Context(), req.Brief)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) listCampaigns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.store.List(r.Context(), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// loadCampaign resolves {id}; it writes the error response and returns false
// on failure.
func (s *Server) loadCampaign(w http.ResponseWriter, r *http.Request) (campaign.Campaign, bool) {
	id, err := campaignID(r)
	if err == nil {
		var c campaign.Campaign
		if c, err = s.store.Get(r.Context(), id); err == nil {
			return c, true
		}
	}
	writeError(w, r, err)
	return campaign.Campaign{}, false
}

func (s *Server) getCampaign(w http.ResponseWriter, r *http.Request) {
	if c, ok := s.loadCampaign(w, r); ok {
		writeJSON(w, http.StatusOK, c)
	}
}

func (s *Server) putChannels(w http.ResponseWriter, r *http.Request) {
	id, err := campaignID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req struct {
		Channels []string `json:"channels"`
	}
	if err := decode(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.store.SetChannels(r.Context(), id, req.Channels); err != nil {
		writeError(w, r, err)
		return
	}
	c, err := s.store.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// putContent validates human-edited content against the audience's worst
// case and saves it.
func (s *Server) putContent(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadCampaign(w, r)
	if !ok {
		return
	}
	var req struct {
		Content content.Content `json:"content"`
	}
	if err := decode(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	if campaign.Locked(c.Status) {
		writeError(w, r, campaign.ErrLocked)
		return
	}
	if c.Rule == nil {
		writeError(w, r, campaign.ErrNotReady)
		return
	}
	wc, err := campaign.WorstCaseFor(r.Context(), s.pool, *c.Rule, s.now())
	if err != nil {
		writeError(w, r, err)
		return
	}
	if errs := content.Validate(req.Content, c.Channels, wc); len(errs) > 0 {
		writeError(w, r, errs)
		return
	}
	if err := s.store.SaveContent(r.Context(), c.ID, req.Content); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": req.Content, "sms_stats": smsStats(req.Content, wc)})
}

// smsStats measures the SMS rendered with the worst-case values.
func smsStats(c content.Content, wc content.WorstCase) content.SMSStats {
	wc = wc.WithDefaults()
	return content.SMSInfo(content.Render(c.SMS, map[string]string{
		"first_name": wc.LongestFirstName, "city": wc.LongestCity,
	}))
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	id, err := campaignID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req campaign.ApproveRequest
	if err := decode(r, &req, true); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := campaign.Approve(r.Context(), s.pool, id, req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
