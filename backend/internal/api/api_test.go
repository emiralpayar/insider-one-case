package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/api"
	"github.com/emiralpayar/campaign-composer/internal/dbtest"
	"github.com/emiralpayar/campaign-composer/internal/llm"
	"github.com/emiralpayar/campaign-composer/internal/seed"
	"github.com/jackc/pgx/v5/pgxpool"
)

const brief = "İstanbul'da son 30 günde sepete ürün ekleyip almayanlara %30 indirim"

func seeded(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.New(t)
	if _, err := seed.Run(context.Background(), pool, seed.Options{Users: 300, Seed: 42, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return pool
}

func mockComposer(pool *pgxpool.Pool) *llm.Composer {
	return &llm.Composer{SegmentClient: llm.NewMock(), ContentClient: llm.NewMock(), Log: llm.NewPGCallLogger(pool)}
}

func newServer(t *testing.T, pool *pgxpool.Pool, c *llm.Composer, provider string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(api.New(api.Config{
		Pool: pool, Composer: c, LLMProvider: provider,
		Models: api.Models{Segment: "seg-model", Content: "content-model"},
	}))
	t.Cleanup(srv.Close)
	return srv
}

type resp struct {
	Code int
	Body []byte
}

func (r resp) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode %s: %v", r.Body, err)
	}
}

func (r resp) errCode(t *testing.T) string {
	t.Helper()
	var e struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	r.decode(t, &e)
	return e.Error.Code
}

func do(t *testing.T, srv *httptest.Server, method, path string, body any) resp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		if s, ok := body.(string); ok {
			rd = strings.NewReader(s)
		} else {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{res.StatusCode, b}
}

type sseEvent struct {
	Type string
	Data json.RawMessage
}

func stream(t *testing.T, srv *httptest.Server, path string) ([]sseEvent, *http.Response) {
	t.Helper()
	res, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var evs []sseEvent
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var cur sseEvent
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.Type = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.Data = json.RawMessage(strings.TrimPrefix(line, "data: "))
		case line == "" && cur.Type != "":
			evs = append(evs, cur)
			cur = sseEvent{}
		}
	}
	return evs, res
}

func createCampaign(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	r := do(t, srv, "POST", "/api/campaigns", map[string]string{"brief": brief})
	if r.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", r.Code, r.Body)
	}
	var c struct {
		ID          string   `json:"id"`
		Status      string   `json:"status"`
		Channels    []string `json:"channels"`
		LLMProvider string   `json:"llm_provider"`
	}
	r.decode(t, &c)
	if c.Status != "draft" || len(c.Channels) != 2 || c.LLMProvider != "mock" {
		t.Fatalf("created: %s", r.Body)
	}
	return c.ID
}

func TestFlow(t *testing.T) {
	pool := seeded(t)
	srv := newServer(t, pool, mockComposer(pool), "mock")
	id := createCampaign(t, srv)

	// generate
	r := do(t, srv, "POST", "/api/campaigns/"+id+"/segment/generate", nil)
	if r.Code != 200 {
		t.Fatalf("generate: %d %s", r.Code, r.Body)
	}
	var gen struct {
		Rule struct {
			Audience   string `json:"audience"`
			Conditions []struct {
				Field string          `json:"field"`
				Value json.RawMessage `json:"value"`
			} `json:"conditions"`
		} `json:"rule"`
		Preview struct {
			Total int `json:"total"`
		} `json:"preview"`
	}
	r.decode(t, &gen)
	fields := map[string]string{}
	for _, c := range gen.Rule.Conditions {
		fields[c.Field] = string(c.Value)
	}
	if !strings.Contains(fields["city"], "İstanbul") || fields["abandoned_cart"] != "30" || gen.Preview.Total == 0 {
		t.Fatalf("generate result: %s", r.Body)
	}

	// refine does not save
	r = do(t, srv, "POST", "/api/campaigns/"+id+"/segment/refine", map[string]string{"instruction": "Ankara'yı da ekle"})
	if r.Code != 200 || !strings.Contains(string(r.Body), "Ankara") || !strings.Contains(string(r.Body), "previous_preview") {
		t.Fatalf("refine: %d %s", r.Code, r.Body)
	}
	r = do(t, srv, "GET", "/api/campaigns/"+id, nil)
	if strings.Contains(string(r.Body), "Ankara") || !strings.Contains(string(r.Body), `"audience_ready"`) {
		t.Fatalf("refine saved the rule: %s", r.Body)
	}

	// content stream
	evs, res := stream(t, srv, "/api/campaigns/"+id+"/content/stream?channel=all")
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}
	partials := 0
	var done struct {
		Content struct {
			SMS string `json:"sms"`
		} `json:"content"`
		Issues   []any `json:"issues"`
		Saved    bool  `json:"saved"`
		SMSStats struct {
			Chars int `json:"chars"`
		} `json:"sms_stats"`
		RenderedSample struct {
			Named    struct{ SMS string } `json:"named"`
			Nameless struct{ SMS string } `json:"nameless"`
		} `json:"rendered_sample"`
	}
	gotDone := false
	for _, e := range evs {
		switch e.Type {
		case "partial":
			partials++
		case "done":
			gotDone = true
			if err := json.Unmarshal(e.Data, &done); err != nil {
				t.Fatal(err)
			}
		case "error":
			t.Fatalf("error event: %s", e.Data)
		}
	}
	if partials < 2 || !gotDone {
		t.Fatalf("partials=%d done=%v events=%v", partials, gotDone, evs)
	}
	if !done.Saved || len(done.Issues) != 0 || !strings.Contains(done.Content.SMS, "{{first_name}}") || done.SMSStats.Chars == 0 {
		t.Fatalf("done = %+v", done)
	}
	if !strings.HasPrefix(done.RenderedSample.Nameless.SMS, "Merhaba, ") || strings.Contains(done.RenderedSample.Named.SMS, "{{") ||
		done.RenderedSample.Named.SMS == done.RenderedSample.Nameless.SMS {
		t.Fatalf("rendered sample = %+v", done.RenderedSample)
	}

	// regenerate SMS only keeps email
	r = do(t, srv, "GET", "/api/campaigns/"+id, nil)
	before := r.Body
	evs, _ = stream(t, srv, "/api/campaigns/"+id+"/content/stream?channel=sms")
	if evs[len(evs)-1].Type != "done" {
		t.Fatalf("sms regen last event: %+v", evs[len(evs)-1])
	}
	var c1, c2 struct {
		Status  string `json:"status"`
		Content struct {
			Email struct{ Subject string } `json:"email"`
		} `json:"content"`
	}
	json.Unmarshal(before, &c1)
	do(t, srv, "GET", "/api/campaigns/"+id, nil).decode(t, &c2)
	if c2.Status != "content_ready" || c2.Content.Email.Subject == "" || c2.Content.Email.Subject != c1.Content.Email.Subject {
		t.Fatalf("after sms regen: %+v (before %+v)", c2, c1)
	}

	// approve
	r = do(t, srv, "POST", "/api/campaigns/"+id+"/approve", map[string]bool{"confirm_large_audience": false})
	if r.Code != 200 {
		t.Fatalf("approve: %d %s", r.Code, r.Body)
	}
	var ap struct {
		Enqueued map[string]int `json:"enqueued"`
	}
	r.decode(t, &ap)

	// second approve → 409
	r = do(t, srv, "POST", "/api/campaigns/"+id+"/approve", nil)
	if r.Code != http.StatusConflict || r.errCode(t) != "already_approved" {
		t.Fatalf("second approve: %d %s", r.Code, r.Body)
	}

	// status
	r = do(t, srv, "GET", "/api/campaigns/"+id+"/status", nil)
	var st struct {
		CampaignStatus string `json:"campaign_status"`
		Channels       map[string]struct {
			Total    int            `json:"total"`
			ByStatus map[string]int `json:"by_status"`
		} `json:"channels"`
		RecentErrors []any `json:"recent_errors"`
	}
	r.decode(t, &st)
	if st.CampaignStatus != "sending" || st.Channels["sms"].Total != ap.Enqueued["sms"] ||
		st.Channels["email"].Total != ap.Enqueued["email"] || ap.Enqueued["sms"] == 0 || st.RecentErrors == nil {
		t.Fatalf("status: %s (enqueued %v)", r.Body, ap.Enqueued)
	}

	// locked after approval
	r = do(t, srv, "PUT", "/api/campaigns/"+id+"/channels", map[string]any{"channels": []string{"sms"}})
	if r.Code != http.StatusConflict || r.errCode(t) != "campaign_locked" {
		t.Fatalf("channels after approve: %d %s", r.Code, r.Body)
	}

	// list
	r = do(t, srv, "GET", "/api/campaigns", nil)
	var list []map[string]any
	r.decode(t, &list)
	if len(list) != 1 {
		t.Fatalf("list: %s", r.Body)
	}

	// metrics
	r = do(t, srv, "GET", "/metrics", nil)
	if r.Code != 200 || !strings.Contains(string(r.Body), `composer_llm_calls_total{purpose="segment",result="ok"} 1`) ||
		!strings.Contains(string(r.Body), `composer_queue_depth{channel="sms",status="queued"}`) {
		t.Fatalf("metrics: %s", r.Body)
	}
}

func TestNoKeyReturns503(t *testing.T) {
	pool := seeded(t)
	srv := newServer(t, pool, nil, "anthropic")
	r := do(t, srv, "POST", "/api/campaigns", map[string]string{"brief": brief})
	if r.Code != http.StatusCreated || !strings.Contains(string(r.Body), `"llm_provider":"anthropic"`) {
		t.Fatalf("create: %d %s", r.Code, r.Body)
	}
	var c struct{ ID string }
	r.decode(t, &c)

	for _, p := range []struct{ method, path string }{
		{"POST", "/api/campaigns/" + c.ID + "/segment/generate"},
		{"POST", "/api/campaigns/" + c.ID + "/segment/refine"},
		{"GET", "/api/campaigns/" + c.ID + "/content/stream?channel=all"},
	} {
		r = do(t, srv, p.method, p.path, map[string]string{"instruction": "x"})
		if r.Code != http.StatusServiceUnavailable || r.errCode(t) != "llm_not_configured" ||
			!strings.Contains(string(r.Body), "ANTHROPIC_API_KEY tanımlı değil") {
			t.Fatalf("%s %s: %d %s", p.method, p.path, r.Code, r.Body)
		}
	}

	r = do(t, srv, "GET", "/api/config", nil)
	var cfg struct {
		LLMProvider   string            `json:"llm_provider"`
		LLMConfigured bool              `json:"llm_configured"`
		Models        map[string]string `json:"models"`
	}
	r.decode(t, &cfg)
	if cfg.LLMProvider != "anthropic" || cfg.LLMConfigured || cfg.Models["segment"] != "seg-model" || cfg.Models["content"] != "content-model" {
		t.Fatalf("config: %s", r.Body)
	}

	r = do(t, srv, "GET", "/readyz", nil)
	if r.Code != 200 || !strings.Contains(string(r.Body), `"llm":"not_configured"`) || !strings.Contains(string(r.Body), `"db":"ok"`) {
		t.Fatalf("readyz: %d %s", r.Code, r.Body)
	}
}

func TestPutSegmentInvalidRule(t *testing.T) {
	pool := seeded(t)
	srv := newServer(t, pool, mockComposer(pool), "mock")
	id := createCampaign(t, srv)
	bad := map[string]any{"rule": map[string]any{
		"audience": "filtered", "conditions": []any{map[string]any{"field": "phone", "op": "eq", "value": "1"}},
		"unmapped": []string{}, "explanation": "",
	}}
	r := do(t, srv, "PUT", "/api/campaigns/"+id+"/segment", bad)
	if r.Code != http.StatusUnprocessableEntity || r.errCode(t) != "validation_failed" || !strings.Contains(string(r.Body), `"details":[`) {
		t.Fatalf("invalid rule: %d %s", r.Code, r.Body)
	}
	r = do(t, srv, "POST", "/api/segments/preview", bad)
	if r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("preview invalid: %d %s", r.Code, r.Body)
	}

	good := map[string]any{"rule": map[string]any{
		"audience": "filtered", "conditions": []any{map[string]any{"field": "city", "op": "in", "value": []string{"Ankara"}}},
		"unmapped": []string{}, "explanation": "Ankara",
	}}
	r = do(t, srv, "PUT", "/api/campaigns/"+id+"/segment", good)
	if r.Code != 200 || !strings.Contains(string(r.Body), `"preview"`) {
		t.Fatalf("valid rule: %d %s", r.Code, r.Body)
	}
	r = do(t, srv, "POST", "/api/segments/preview", good)
	if r.Code != 200 || !strings.Contains(string(r.Body), `"total"`) {
		t.Fatalf("preview: %d %s", r.Code, r.Body)
	}

	// A filtered rule with no conditions is rejected ("all" must be explicit).
	none := map[string]any{"rule": map[string]any{"audience": "filtered", "conditions": []any{}, "unmapped": []string{}, "explanation": ""}}
	r = do(t, srv, "PUT", "/api/campaigns/"+id+"/segment", none)
	if r.Code != http.StatusUnprocessableEntity || !strings.Contains(string(r.Body), `"empty_conditions"`) {
		t.Fatalf("empty filtered rule: %d %s", r.Code, r.Body)
	}
	// The explanation is saved as given: a hand-edited rule clears it.
	good["rule"].(map[string]any)["explanation"] = ""
	if r = do(t, srv, "PUT", "/api/campaigns/"+id+"/segment", good); r.Code != 200 {
		t.Fatalf("hand-edited rule: %d %s", r.Code, r.Body)
	}
	if r = do(t, srv, "GET", "/api/campaigns/"+id, nil); !strings.Contains(string(r.Body), `"explanation":""`) {
		t.Fatalf("explanation not cleared: %s", r.Body)
	}

	// content PUT: too-long SMS rejected, valid saved
	long := map[string]any{"content": map[string]any{"sms": strings.Repeat("a", 170),
		"email": map[string]string{"subject": "s", "preheader": "", "body": "b"}}}
	r = do(t, srv, "PUT", "/api/campaigns/"+id+"/content", long)
	if r.Code != http.StatusUnprocessableEntity || r.errCode(t) != "validation_failed" {
		t.Fatalf("long content: %d %s", r.Code, r.Body)
	}
	ok := map[string]any{"content": map[string]any{"sms": "Merhaba {{first_name}}, fırsat!",
		"email": map[string]string{"subject": "s", "preheader": "", "body": "b"}}}
	r = do(t, srv, "PUT", "/api/campaigns/"+id+"/content", ok)
	if r.Code != 200 || !strings.Contains(string(r.Body), `"sms_stats"`) {
		t.Fatalf("content: %d %s", r.Code, r.Body)
	}

	// bad JSON, unknown campaign, bad channels
	if r = do(t, srv, "POST", "/api/campaigns", "{nope"); r.Code != http.StatusBadRequest || r.errCode(t) != "bad_request" {
		t.Fatalf("bad json: %d %s", r.Code, r.Body)
	}
	if r = do(t, srv, "GET", "/api/campaigns/00000000-0000-0000-0000-000000000001", nil); r.Code != 404 || r.errCode(t) != "not_found" {
		t.Fatalf("missing: %d %s", r.Code, r.Body)
	}
	if r = do(t, srv, "GET", "/api/campaigns/not-a-uuid", nil); r.Code != 404 {
		t.Fatalf("bad uuid: %d %s", r.Code, r.Body)
	}
	if r = do(t, srv, "PUT", "/api/campaigns/"+id+"/channels", map[string]any{"channels": []string{"fax"}}); r.Code != 422 {
		t.Fatalf("bad channels: %d %s", r.Code, r.Body)
	}
	if r = do(t, srv, "PUT", "/api/campaigns/"+id+"/channels", map[string]any{"channels": []string{"email"}}); r.Code != 200 ||
		!strings.Contains(string(r.Body), `"channels":["email"]`) {
		t.Fatalf("channels: %d %s", r.Code, r.Body)
	}
}

func TestGenerateInvalidOutput(t *testing.T) {
	pool := seeded(t)
	bad := `{"audience":"filtered","conditions":[{"field":"phone","op":"eq","value":"x"}],"unmapped":[],"explanation":""}`
	c := &llm.Composer{SegmentClient: llm.NewScripted(bad, bad, bad), ContentClient: llm.NewMock()}
	srv := newServer(t, pool, c, "mock")
	id := createCampaign(t, srv)
	r := do(t, srv, "POST", "/api/campaigns/"+id+"/segment/generate", nil)
	if r.Code != http.StatusUnprocessableEntity || r.errCode(t) != "llm_invalid_output" || !strings.Contains(string(r.Body), `"details":[`) {
		t.Fatalf("generate: %d %s", r.Code, r.Body)
	}
	r = do(t, srv, "GET", "/api/campaigns/"+id, nil)
	if !strings.Contains(string(r.Body), `"status":"draft"`) {
		t.Fatalf("campaign changed: %s", r.Body)
	}
	// Script exhausted → a non-validation client error → 502 llm_error.
	r = do(t, srv, "POST", "/api/campaigns/"+id+"/segment/generate", nil)
	if r.Code != http.StatusBadGateway || r.errCode(t) != "llm_error" {
		t.Fatalf("generate after exhaustion: %d %s", r.Code, r.Body)
	}
}

func TestContentStreamInvalidOutput(t *testing.T) {
	pool := seeded(t)
	long := `{"sms":"` + strings.Repeat("a", 200) + `","email":{"subject":"s","preheader":"","body":"b"}}`
	c := &llm.Composer{SegmentClient: llm.NewMock(), ContentClient: llm.NewScripted(long, long, long)}
	srv := newServer(t, pool, c, "mock")
	id := createCampaign(t, srv)
	if r := do(t, srv, "POST", "/api/campaigns/"+id+"/segment/generate", nil); r.Code != 200 {
		t.Fatalf("generate: %d %s", r.Code, r.Body)
	}
	evs, _ := stream(t, srv, "/api/campaigns/"+id+"/content/stream?channel=all")
	repairs := 0
	for _, e := range evs {
		if e.Type == "repair" {
			repairs++
		}
	}
	last := evs[len(evs)-1]
	if repairs != 2 || last.Type != "error" || !strings.Contains(string(last.Data), "llm_invalid_output") ||
		!strings.Contains(string(last.Data), "sms_too_long") {
		t.Fatalf("repairs=%d last=%s %s", repairs, last.Type, last.Data)
	}
	r := do(t, srv, "GET", "/api/campaigns/"+id, nil)
	if !strings.Contains(string(r.Body), `"status":"audience_ready"`) || !strings.Contains(string(r.Body), `"content":null`) {
		t.Fatalf("campaign after failed stream: %s", r.Body)
	}
}

func TestHealth(t *testing.T) {
	pool := seeded(t)
	srv := newServer(t, pool, mockComposer(pool), "mock")
	if r := do(t, srv, "GET", "/healthz", nil); r.Code != 200 {
		t.Fatalf("healthz %d", r.Code)
	}
	r := do(t, srv, "GET", "/readyz", nil)
	if r.Code != 200 || !strings.Contains(string(r.Body), `"llm":"mock"`) {
		t.Fatalf("readyz: %d %s", r.Code, r.Body)
	}
	r = do(t, srv, "GET", "/api/config", nil)
	if !strings.Contains(string(r.Body), `"llm_configured":true`) {
		t.Fatalf("config: %s", r.Body)
	}
	pool.Close()
	if r = do(t, srv, "GET", "/readyz", nil); r.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with closed pool: %d %s", r.Code, r.Body)
	}
}

// ctxIgnoringClient streams a prefix, waits for the request to be cancelled
// and then returns perfectly valid content with a nil error anyway — so only
// the handler's own ctx check can prevent the save.
type ctxIgnoringClient struct {
	returned chan struct{}
	delay    time.Duration // when set: sleep instead of waiting for cancellation
}

func (ctxIgnoringClient) Name() string { return "ctx-ignoring" }

const validContentJSON = `{"sms":"Merhaba {{first_name}}, fırsat seni bekliyor!","email":{"subject":"Fırsat","preheader":"","body":"Merhaba {{first_name}}"}}`

func (b ctxIgnoringClient) Complete(ctx context.Context, req llm.Request, onDelta func(string)) (llm.Response, error) {
	if req.Purpose != "content" {
		return llm.NewMock().Complete(ctx, req, onDelta)
	}
	onDelta(validContentJSON[:20])
	if b.delay > 0 {
		time.Sleep(b.delay)
	} else {
		<-ctx.Done()
	}
	onDelta(validContentJSON[20:])
	if b.returned != nil {
		close(b.returned)
	}
	return llm.Response{Text: validContentJSON, Model: "ctx-ignoring"}, nil
}

func TestContentStreamClientDisconnect(t *testing.T) {
	pool := seeded(t)
	bc := ctxIgnoringClient{returned: make(chan struct{})}
	srv := newServer(t, pool, &llm.Composer{SegmentClient: bc, ContentClient: bc}, "mock")
	id := createCampaign(t, srv)
	if r := do(t, srv, "POST", "/api/campaigns/"+id+"/segment/generate", nil); r.Code != 200 {
		t.Fatalf("generate: %d %s", r.Code, r.Body)
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/campaigns/"+id+"/content/stream", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(res.Body).ReadString('\n')
	if err != nil || line != "event: partial\n" {
		t.Fatalf("first line %q err %v", line, err)
	}
	cancel()
	res.Body.Close()
	select {
	case <-bc.returned:
	case <-time.After(5 * time.Second):
		t.Fatal("server never noticed the disconnect")
	}
	// The client returned valid content; give the handler time to (wrongly) save.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		r := do(t, srv, "GET", "/api/campaigns/"+id, nil)
		if !strings.Contains(string(r.Body), `"content":null`) {
			t.Fatalf("content saved after disconnect: %s", r.Body)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestContentStreamHeartbeat(t *testing.T) {
	pool := seeded(t)
	bc := ctxIgnoringClient{delay: 300 * time.Millisecond}
	srv := httptest.NewServer(api.New(api.Config{
		Pool: pool, Composer: &llm.Composer{SegmentClient: bc, ContentClient: bc}, LLMProvider: "mock",
		Heartbeat: 50 * time.Millisecond,
	}))
	t.Cleanup(srv.Close)
	id := createCampaign(t, srv)
	if r := do(t, srv, "POST", "/api/campaigns/"+id+"/segment/generate", nil); r.Code != 200 {
		t.Fatalf("generate: %d %s", r.Code, r.Body)
	}
	res, err := http.Get(srv.URL + "/api/campaigns/" + id + "/content/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), ": ping\n\n") || !strings.Contains(string(body), "event: done\n") ||
		!strings.Contains(string(body), `"saved":true`) {
		t.Fatalf("stream body: %s", body)
	}
}

// Same brief twice with the deterministic mock gives the same text to the
// same people: the second approval needs confirm_duplicate.
func TestDuplicateSendNeedsConfirm(t *testing.T) {
	pool := seeded(t)
	srv := newServer(t, pool, mockComposer(pool), "mock")
	ready := func() string {
		id := createCampaign(t, srv)
		if r := do(t, srv, "POST", "/api/campaigns/"+id+"/segment/generate", nil); r.Code != 200 {
			t.Fatalf("generate: %d %s", r.Code, r.Body)
		}
		if evs, _ := stream(t, srv, "/api/campaigns/"+id+"/content/stream"); evs[len(evs)-1].Type != "done" {
			t.Fatalf("content: %+v", evs[len(evs)-1])
		}
		return id
	}
	first, second := ready(), ready()
	if r := do(t, srv, "POST", "/api/campaigns/"+first+"/approve", map[string]bool{}); r.Code != 200 {
		t.Fatalf("approve first: %d %s", r.Code, r.Body)
	}
	var d struct {
		Duplicates []struct {
			CampaignID string `json:"campaign_id"`
			Recipients int    `json:"recipients"`
		} `json:"duplicates"`
	}
	do(t, srv, "GET", "/api/campaigns/"+second+"/duplicates", nil).decode(t, &d)
	if len(d.Duplicates) == 0 || d.Duplicates[0].CampaignID != first || d.Duplicates[0].Recipients == 0 {
		t.Fatalf("duplicates = %+v", d)
	}
	r := do(t, srv, "POST", "/api/campaigns/"+second+"/approve", map[string]bool{})
	if r.Code != http.StatusUnprocessableEntity || r.errCode(t) != "duplicate_not_confirmed" {
		t.Fatalf("approve second: %d %s", r.Code, r.Body)
	}
	if r := do(t, srv, "POST", "/api/campaigns/"+second+"/approve", map[string]bool{"confirm_duplicate": true}); r.Code != 200 {
		t.Fatalf("approve second confirmed: %d %s", r.Code, r.Body)
	}
}

// DUPLICATE_LLM_CHECK on: a reworded brief that reached the same people is
// reported with the model's reason (mock: shared words); if the model fails,
// the endpoint still answers with the exact checks and similarity_check=error.
func TestDuplicateLLMCheck(t *testing.T) {
	pool := seeded(t)
	setup := newServer(t, pool, mockComposer(pool), "mock")
	first := createCampaign(t, setup)
	if r := do(t, setup, "POST", "/api/campaigns/"+first+"/segment/generate", nil); r.Code != 200 {
		t.Fatalf("generate: %d %s", r.Code, r.Body)
	}
	if evs, _ := stream(t, setup, "/api/campaigns/"+first+"/content/stream"); evs[len(evs)-1].Type != "done" {
		t.Fatalf("content: %+v", evs[len(evs)-1])
	}
	if r := do(t, setup, "POST", "/api/campaigns/"+first+"/approve", map[string]bool{"confirm_large_audience": true}); r.Code != 200 {
		t.Fatalf("approve: %d %s", r.Code, r.Body)
	}
	var fc struct {
		Rule json.RawMessage `json:"rule"`
	}
	do(t, setup, "GET", "/api/campaigns/"+first, nil).decode(t, &fc)

	// Same audience, reworded brief, different text: no exact match.
	r := do(t, setup, "POST", "/api/campaigns", map[string]string{"brief": brief + " Kısa tut."})
	var sc struct {
		ID string `json:"id"`
	}
	r.decode(t, &sc)
	second := sc.ID
	if r := do(t, setup, "PUT", "/api/campaigns/"+second+"/segment", map[string]json.RawMessage{"rule": fc.Rule}); r.Code != 200 {
		t.Fatalf("put rule: %d %s", r.Code, r.Body)
	}
	content := map[string]any{"content": map[string]any{
		"sms":   "Merhaba {{first_name}}, sepetin seni bekliyor.",
		"email": map[string]string{"subject": "Sepetin seni bekliyor", "preheader": "", "body": "Merhaba {{first_name}}, sepetin seni bekliyor."},
	}}
	if r := do(t, setup, "PUT", "/api/campaigns/"+second+"/content", content); r.Code != 200 {
		t.Fatalf("put content: %d %s", r.Code, r.Body)
	}

	type report struct {
		Duplicates []struct {
			CampaignID string `json:"campaign_id"`
			Match      string `json:"match"`
			Reason     string `json:"reason"`
		} `json:"duplicates"`
		SimilarityCheck string `json:"similarity_check"`
	}
	var off report
	do(t, setup, "GET", "/api/campaigns/"+second+"/duplicates", nil).decode(t, &off)
	if off.SimilarityCheck != "off" || len(off.Duplicates) != 0 {
		t.Fatalf("flag off: %+v", off)
	}

	on := httptest.NewServer(api.New(api.Config{Pool: pool, Composer: mockComposer(pool), LLMProvider: "mock", DuplicateLLMCheck: true}))
	t.Cleanup(on.Close)
	var got report
	do(t, on, "GET", "/api/campaigns/"+second+"/duplicates", nil).decode(t, &got)
	if got.SimilarityCheck != "ok" || len(got.Duplicates) == 0 {
		t.Fatalf("flag on: %+v", got)
	}
	for _, d := range got.Duplicates {
		if d.CampaignID != first || d.Match != "similar" || !strings.HasPrefix(d.Reason, "Ortak kelimeler") {
			t.Fatalf("duplicate: %+v", d)
		}
	}

	bad := `{"matches":[]}` // never names the candidate: fails validation 3 times
	broken := httptest.NewServer(api.New(api.Config{Pool: pool, LLMProvider: "mock", DuplicateLLMCheck: true,
		Composer: &llm.Composer{SegmentClient: llm.NewScripted(bad, bad, bad), ContentClient: llm.NewMock()}}))
	t.Cleanup(broken.Close)
	var e report
	res := do(t, broken, "GET", "/api/campaigns/"+second+"/duplicates", nil)
	res.decode(t, &e)
	if res.Code != 200 || e.SimilarityCheck != "error" || len(e.Duplicates) != 0 {
		t.Fatalf("broken LLM: %d %+v", res.Code, e)
	}
	// Approval is not blocked by the LLM-only match.
	if r := do(t, on, "POST", "/api/campaigns/"+second+"/approve", map[string]bool{"confirm_large_audience": true}); r.Code != 200 {
		t.Fatalf("approve second: %d %s", r.Code, r.Body)
	}
}

func TestHugePageIsRejectedNot500(t *testing.T) {
	pool := seeded(t)
	srv := newServer(t, pool, mockComposer(pool), "mock")
	id := createCampaign(t, srv)
	if r := do(t, srv, "GET", "/api/campaigns/"+id+"/messages?page=9223372036854775807", nil); r.Code >= 500 {
		t.Fatalf("messages: %d %s", r.Code, r.Body)
	}
	rule := map[string]any{"audience": "all", "conditions": []any{}, "unmapped": []any{}}
	if r := do(t, srv, "POST", "/api/segments/members", map[string]any{"rule": rule, "page": 9223372036854775807}); r.Code >= 500 {
		t.Fatalf("members: %d %s", r.Code, r.Body)
	}
}
