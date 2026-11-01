package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestSegmentMembers(t *testing.T) {
	pool := seeded(t)
	srv := newServer(t, pool, mockComposer(pool), "mock")
	rule := map[string]any{
		"audience": "filtered", "conditions": []any{map[string]any{"field": "city", "op": "in", "value": []string{"İstanbul"}}},
		"unmapped": []string{}, "explanation": "",
	}
	var pv struct{ Total int }
	do(t, srv, "POST", "/api/segments/preview", map[string]any{"rule": rule}).decode(t, &pv)

	r := do(t, srv, "POST", "/api/segments/members", map[string]any{"rule": rule})
	if r.Code != 200 {
		t.Fatalf("members: %d %s", r.Code, r.Body)
	}
	var pg struct {
		Members []struct {
			ID         int64   `json:"id"`
			FirstName  string  `json:"first_name"`
			City       string  `json:"city"`
			Language   string  `json:"language"`
			SMSConsent *bool   `json:"sms_consent"`
			OrderCount int     `json:"order_count"`
			TotalSpent float64 `json:"total_spent"`
		} `json:"members"`
		Total   int `json:"total"`
		Page    int `json:"page"`
		PerPage int `json:"per_page"`
	}
	r.decode(t, &pg)
	if pg.Total != pv.Total || pg.Total == 0 || pg.Page != 1 || pg.PerPage != 50 || len(pg.Members) != min(pg.Total, 50) ||
		pg.Members[0].City != "İstanbul" || pg.Members[0].Language == "" {
		t.Fatalf("page 1: %s (preview total %d)", r.Body, pv.Total)
	}
	if pg.Total > 50 {
		r = do(t, srv, "POST", "/api/segments/members", map[string]any{"rule": rule, "page": 2})
		var p2 struct {
			Members []struct{ ID int64 } `json:"members"`
			Page    int                  `json:"page"`
		}
		r.decode(t, &p2)
		if p2.Page != 2 || len(p2.Members) == 0 || p2.Members[0].ID <= pg.Members[len(pg.Members)-1].ID {
			t.Fatalf("page 2: %s", r.Body)
		}
	}
	r = do(t, srv, "POST", "/api/segments/members", map[string]any{"rule": rule, "search": "'; drop table users; --"})
	if r.Code != 200 || !strings.Contains(string(r.Body), `"total":0`) {
		t.Fatalf("injection search: %d %s", r.Code, r.Body)
	}

	bad := map[string]any{"audience": "filtered", "conditions": []any{map[string]any{"field": "phone", "op": "eq", "value": "1"}}, "unmapped": []string{}}
	if r = do(t, srv, "POST", "/api/segments/members", map[string]any{"rule": bad}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid rule: %d %s", r.Code, r.Body)
	}
	if r = do(t, srv, "POST", "/api/segments/members", map[string]any{"rule": rule, "page": -1}); r.Code != http.StatusBadRequest {
		t.Fatalf("negative page: %d %s", r.Code, r.Body)
	}
}
