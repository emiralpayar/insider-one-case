package llm

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/dbtest"
	"github.com/emiralpayar/campaign-composer/internal/verr"
	"github.com/google/uuid"
)

func TestPGCallLogger(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	l := NewPGCallLogger(pool)
	id := uuid.New()
	errs := verr.List{{Path: "conditions[0].value", Code: "unknown_city", Message: "city \"Istanbull\" is not a known city"}}
	err := l.Log(ctx, CallRecord{
		CampaignID: &id, Purpose: "segment", Attempt: 2,
		Request:          map[string]any{"messages": []Message{{Role: "user", Text: "brief"}}},
		ResponseText:     `{"audience":"filtered"}`,
		ValidationErrors: errs, Model: "claude-sonnet-5-5",
		InputTokens: 120, OutputTokens: 45, Latency: 1500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Log(ctx, CallRecord{Purpose: "content", Attempt: 1, Err: "boom"}); err != nil {
		t.Fatal(err)
	}

	var (
		cid                  *uuid.UUID
		purpose, resp, model string
		attempt, in, out, ms int
		reqJSON, veJSON      []byte
	)
	err = pool.QueryRow(ctx, `select campaign_id, purpose, attempt, request, response_text, validation_errors,
		model, input_tokens, output_tokens, latency_ms from llm_calls where purpose='segment'`).
		Scan(&cid, &purpose, &attempt, &reqJSON, &resp, &veJSON, &model, &in, &out, &ms)
	if err != nil {
		t.Fatal(err)
	}
	if cid == nil || *cid != id || attempt != 2 || resp != `{"audience":"filtered"}` || model != "claude-sonnet-5-5" ||
		in != 120 || out != 45 || ms != 1500 {
		t.Fatalf("row = %v %s %d %s %s %d %d %d", cid, purpose, attempt, resp, model, in, out, ms)
	}
	var gotErrs verr.List
	if err := json.Unmarshal(veJSON, &gotErrs); err != nil {
		t.Fatal(err)
	}
	if len(gotErrs) != 1 || gotErrs[0] != errs[0] {
		t.Fatalf("validation_errors = %s", veJSON)
	}
	var req map[string]any
	if err := json.Unmarshal(reqJSON, &req); err != nil || req["messages"] == nil {
		t.Fatalf("request = %s (%v)", reqJSON, err)
	}

	var errText string
	var nullCID *uuid.UUID
	var nullVE []byte
	if err := pool.QueryRow(ctx, `select campaign_id, error, validation_errors from llm_calls where purpose='content'`).
		Scan(&nullCID, &errText, &nullVE); err != nil {
		t.Fatal(err)
	}
	if nullCID != nil || errText != "boom" || nullVE != nil {
		t.Fatalf("content row = %v %q %s", nullCID, errText, nullVE)
	}
}
