package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestCampaignName(t *testing.T) {
	pool := seeded(t)
	srv := newServer(t, pool, mockComposer(pool), "mock")
	id := createCampaign(t, srv)

	var c struct {
		Name  string `json:"name"`
		Brief string `json:"brief"`
	}
	do(t, srv, "GET", "/api/campaigns/"+id, nil).decode(t, &c)
	if c.Name == "" || !strings.HasPrefix(c.Brief, strings.TrimSuffix(c.Name, "…")) {
		t.Fatalf("default name %q for brief %q", c.Name, c.Brief)
	}

	r := do(t, srv, "PUT", "/api/campaigns/"+id+"/name", map[string]string{"name": "  İstanbul sepet  "})
	if r.Code != 200 {
		t.Fatalf("rename: %d %s", r.Code, r.Body)
	}
	r.decode(t, &c)
	if c.Name != "İstanbul sepet" {
		t.Fatalf("renamed: %s", r.Body)
	}
	var list []struct{ Name string }
	do(t, srv, "GET", "/api/campaigns", nil).decode(t, &list)
	if len(list) != 1 || list[0].Name != "İstanbul sepet" {
		t.Fatalf("list: %+v", list)
	}

	for _, bad := range []string{"", "   ", strings.Repeat("a", 121)} {
		r = do(t, srv, "PUT", "/api/campaigns/"+id+"/name", map[string]string{"name": bad})
		if r.Code != http.StatusUnprocessableEntity || r.errCode(t) != "validation_failed" {
			t.Fatalf("name %q: %d %s", bad, r.Code, r.Body)
		}
	}
	if r = do(t, srv, "PUT", "/api/campaigns/00000000-0000-0000-0000-000000000001/name", map[string]string{"name": "x"}); r.Code != 404 {
		t.Fatalf("missing: %d %s", r.Code, r.Body)
	}
	if r = do(t, srv, "PUT", "/api/campaigns/"+id+"/name", "{nope"); r.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d %s", r.Code, r.Body)
	}
}
