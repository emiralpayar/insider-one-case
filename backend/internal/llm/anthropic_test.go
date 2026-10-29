package llm

import (
	"encoding/json"
	"testing"

	"github.com/emiralpayar/campaign-composer/internal/segment"
)

func TestAnthropicParams(t *testing.T) {
	req := Request{
		Purpose: "segment", System: "sys",
		Messages: []Message{{Role: "user", Text: "brief"}, {Role: "assistant", Text: "bad"}, {Role: "user", Text: "fix"}},
		Schema:   segment.JSONSchema(),
		Hint:     map[string]any{"brief": "must not be sent"},
	}
	raw, err := json.Marshal(buildParams("claude-sonnet-5-5", req))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "claude-sonnet-5-5" || got["max_tokens"] == nil {
		t.Fatalf("params = %s", raw)
	}
	for _, k := range []string{"temperature", "top_p", "top_k", "tool_choice", "tools"} {
		if _, ok := got[k]; ok {
			t.Fatalf("must not send %s: %s", k, raw)
		}
	}
	oc := got["output_config"].(map[string]any)
	if oc["effort"] != "low" {
		t.Fatalf("effort = %v", oc["effort"])
	}
	f := oc["format"].(map[string]any)
	if f["type"] != "json_schema" || f["schema"].(map[string]any)["type"] != "object" {
		t.Fatalf("format = %v", f)
	}
	th := got["thinking"].(map[string]any)
	if th["type"] != "adaptive" || th["budget_tokens"] != nil {
		t.Fatalf("thinking = %v", th)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 || msgs[1].(map[string]any)["role"] != "assistant" || msgs[2].(map[string]any)["role"] != "user" {
		t.Fatalf("messages = %v", msgs)
	}
	sys := got["system"].([]any)
	if sys[0].(map[string]any)["text"] != "sys" {
		t.Fatalf("system = %v", sys)
	}
	if string(raw) == "" || json.Valid(raw) == false {
		t.Fatal("invalid json")
	}

	req.Purpose = "content"
	raw, _ = json.Marshal(buildParams("m", req))
	_ = json.Unmarshal(raw, &got)
	if got["output_config"].(map[string]any)["effort"] != "medium" {
		t.Fatalf("content effort: %s", raw)
	}
}
