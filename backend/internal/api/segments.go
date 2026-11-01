package api

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/emiralpayar/campaign-composer/internal/verr"
)

type ruleResponse struct {
	Rule            segment.Rule           `json:"rule"`
	Preview         segment.PreviewResult  `json:"preview"`
	PreviousPreview *segment.PreviewResult `json:"previous_preview,omitempty"`
}

// generateSegment asks the LLM for a rule from the brief and saves it.
func (s *Server) generateSegment(w http.ResponseWriter, r *http.Request) {
	if !s.requireLLM(w, r) {
		return
	}
	c, ok := s.loadCampaign(w, r)
	if !ok {
		return
	}
	if campaign.Locked(c.Status) {
		writeError(w, r, campaign.ErrLocked)
		return
	}
	rule, err := s.composer.Segment(r.Context(), c.ID, c.Brief)
	if err != nil {
		writeError(w, r, llmFailure(err))
		return
	}
	s.saveRuleAndPreview(w, r, c, rule)
}

// refineSegment returns an LLM-edited proposal with old and new previews; it
// does not save (the marketer accepts it with PUT /segment).
func (s *Server) refineSegment(w http.ResponseWriter, r *http.Request) {
	if !s.requireLLM(w, r) {
		return
	}
	c, ok := s.loadCampaign(w, r)
	if !ok {
		return
	}
	var req struct {
		Instruction string `json:"instruction"`
	}
	if err := decode(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	req.Instruction = strings.TrimSpace(req.Instruction)
	if req.Instruction == "" {
		writeError(w, r, verr.List{{Path: "instruction", Code: "empty_field", Message: "Talimat boş olamaz"}})
		return
	}
	if utf8.RuneCountInString(req.Instruction) > maxBriefRunes {
		writeError(w, r, verr.List{{Path: "instruction", Code: "too_long", Message: fmt.Sprintf("Talimat en fazla %d karakter olabilir", maxBriefRunes)}})
		return
	}
	if c.Rule == nil {
		writeError(w, r, campaign.ErrNotReady)
		return
	}
	if campaign.Locked(c.Status) {
		writeError(w, r, campaign.ErrLocked)
		return
	}
	asOf := s.now()
	prev, err := segment.Preview(r.Context(), s.pool, *c.Rule, asOf)
	if err != nil {
		writeError(w, r, err)
		return
	}
	rule, err := s.composer.Refine(r.Context(), c.ID, c.Brief, *c.Rule, req.Instruction)
	if err != nil {
		writeError(w, r, llmFailure(err))
		return
	}
	pv, err := segment.Preview(r.Context(), s.pool, rule, asOf)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ruleResponse{Rule: rule, Preview: pv, PreviousPreview: &prev})
}

// putSegment validates a form-edited (or accepted refine) rule and saves it.
func (s *Server) putSegment(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadCampaign(w, r)
	if !ok {
		return
	}
	var req struct {
		Rule segment.Rule `json:"rule"`
	}
	if err := decode(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	rule, errs := segment.Validate(req.Rule)
	if len(errs) > 0 {
		writeError(w, r, errs)
		return
	}
	s.saveRuleAndPreview(w, r, c, rule)
}

func (s *Server) saveRuleAndPreview(w http.ResponseWriter, r *http.Request, c campaign.Campaign, rule segment.Rule) {
	if err := s.store.SaveRule(r.Context(), c.ID, rule); err != nil {
		writeError(w, r, err)
		return
	}
	pv, err := segment.Preview(r.Context(), s.pool, rule, s.now())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ruleResponse{Rule: rule, Preview: pv})
}

// previewSegment counts the audience of an unsaved rule.
func (s *Server) previewSegment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rule segment.Rule `json:"rule"`
	}
	if err := decode(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	rule, errs := segment.Validate(req.Rule)
	if len(errs) > 0 {
		writeError(w, r, errs)
		return
	}
	pv, err := segment.Preview(r.Context(), s.pool, rule, s.now())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// segmentMembers lists the users of an unsaved rule, a page at a time
// ("Kitleyi gör"). The rule is validated and compiled as for preview.
func (s *Server) segmentMembers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rule   segment.Rule `json:"rule"`
		Page   int          `json:"page"`
		Search string       `json:"search"`
	}
	if err := decode(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	rule, errs := segment.Validate(req.Rule)
	if len(errs) > 0 {
		writeError(w, r, errs)
		return
	}
	if req.Page < 0 {
		writeError(w, r, badRequest("page pozitif bir tam sayı olmalı"))
		return
	}
	res, err := segment.Members(r.Context(), s.pool, rule, s.now(), segment.MemberQuery{Page: req.Page, Search: req.Search})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
