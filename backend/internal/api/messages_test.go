package api_test

import (
	"context"
	"net/http"
	"testing"
)

func TestCampaignMessages(t *testing.T) {
	pool := seeded(t)
	srv := newServer(t, pool, mockComposer(pool), "mock")
	id := createCampaign(t, srv)
	if r := do(t, srv, "POST", "/api/campaigns/"+id+"/segment/generate", nil); r.Code != 200 {
		t.Fatalf("generate: %d %s", r.Code, r.Body)
	}
	if evs, _ := stream(t, srv, "/api/campaigns/"+id+"/content/stream?channel=all"); evs[len(evs)-1].Type != "done" {
		t.Fatalf("content: %+v", evs[len(evs)-1])
	}
	r := do(t, srv, "POST", "/api/campaigns/"+id+"/approve", nil)
	if r.Code != 200 {
		t.Fatalf("approve: %d %s", r.Code, r.Body)
	}
	var ap struct {
		Enqueued map[string]int `json:"enqueued"`
	}
	r.decode(t, &ap)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `update messages set status='failed', last_error='provider 400', attempts=1
		where id = (select id from messages where campaign_id=$1 and channel='sms' order by user_id limit 1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update messages set status='skipped_no_consent'
		where id = (select id from messages where campaign_id=$1 and channel='email' order by user_id limit 1)`, id); err != nil {
		t.Fatal(err)
	}

	type page struct {
		Messages []struct {
			FirstName string  `json:"first_name"`
			Channel   string  `json:"channel"`
			Status    string  `json:"status"`
			Attempts  int     `json:"attempts"`
			LastError *string `json:"last_error"`
			SentAt    *string `json:"sent_at"`
		} `json:"messages"`
		Total   int `json:"total"`
		Page    int `json:"page"`
		PerPage int `json:"per_page"`
	}
	var all page
	do(t, srv, "GET", "/api/campaigns/"+id+"/messages", nil).decode(t, &all)
	if all.Total != ap.Enqueued["sms"]+ap.Enqueued["email"] || all.Page != 1 || all.PerPage != 50 {
		t.Fatalf("all: %+v (enqueued %v)", all, ap.Enqueued)
	}

	var notSent page
	r = do(t, srv, "GET", "/api/campaigns/"+id+"/messages?status=failed,unknown&status=skipped_no_consent", nil)
	r.decode(t, &notSent)
	if notSent.Total != 2 {
		t.Fatalf("not sent: %s", r.Body)
	}
	var smsFailed page
	do(t, srv, "GET", "/api/campaigns/"+id+"/messages?status=failed&channel=sms", nil).decode(t, &smsFailed)
	if smsFailed.Total != 1 || smsFailed.Messages[0].LastError == nil || *smsFailed.Messages[0].LastError != "provider 400" {
		t.Fatalf("sms failed: %+v", smsFailed)
	}
	var p2 page
	do(t, srv, "GET", "/api/campaigns/"+id+"/messages?page=2", nil).decode(t, &p2)
	if p2.Page != 2 || len(p2.Messages) != min(max(all.Total-50, 0), 50) {
		t.Fatalf("page 2: page=%d len=%d total=%d", p2.Page, len(p2.Messages), all.Total)
	}

	for path, code := range map[string]int{
		"/messages?status=bogus": http.StatusUnprocessableEntity,
		"/messages?channel=fax":  http.StatusUnprocessableEntity,
		"/messages?page=0":       http.StatusBadRequest,
		"/messages?page=abc":     http.StatusBadRequest,
	} {
		if r = do(t, srv, "GET", "/api/campaigns/"+id+path, nil); r.Code != code {
			t.Fatalf("%s: %d %s", path, r.Code, r.Body)
		}
	}
	if r = do(t, srv, "GET", "/api/campaigns/00000000-0000-0000-0000-000000000001/messages", nil); r.Code != 404 {
		t.Fatalf("missing: %d %s", r.Code, r.Body)
	}
}
