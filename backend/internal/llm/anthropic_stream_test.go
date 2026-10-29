package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func sse(event string, data any) string {
	b, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, b)
}

// cannedStream builds a Messages API SSE stream with the given text chunks and
// stop reason. If cut is true the stream ends after the deltas (no
// message_delta / message_stop).
func cannedStream(chunks []string, stopReason string, cut bool) string {
	var b strings.Builder
	b.WriteString(sse("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-sonnet-5-5", "content": []any{},
		"stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 42, "output_tokens": 1},
	}}))
	b.WriteString(sse("content_block_start", map[string]any{"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "text", "text": ""}}))
	for _, c := range chunks {
		b.WriteString(sse("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": c}}))
	}
	if cut {
		b.WriteString("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n")
		return b.String()
	}
	b.WriteString(sse("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}))
	b.WriteString(sse("message_delta", map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 17}}))
	b.WriteString(sse("message_stop", map[string]any{"type": "message_stop"}))
	return b.String()
}

func fakeAnthropic(t *testing.T, body string) Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req["stream"] != true {
			t.Errorf("request not streaming: %v %v", req["stream"], err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &anthropicClient{
		client: anthropic.NewClient(option.WithAPIKey("test"), option.WithBaseURL(srv.URL), option.WithMaxRetries(0)),
		model:  "claude-sonnet-5-5",
	}
}

var segReq = Request{Purpose: "segment", System: "s", Messages: []Message{{Role: "user", Text: "b"}}, Schema: map[string]any{"type": "object"}}

func TestAnthropicStreamEndTurn(t *testing.T) {
	c := fakeAnthropic(t, cannedStream([]string{`{"audience":`, `"all"}`}, "end_turn", false))
	var deltas []string
	resp, err := c.Complete(context.Background(), segReq, func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != `{"audience":"all"}` || resp.StopReason != "end_turn" || resp.Model != "claude-sonnet-5-5" ||
		resp.InputTokens != 42 || resp.OutputTokens != 17 {
		t.Fatalf("resp = %+v", resp)
	}
	if strings.Join(deltas, "|") != `{"audience":|"all"}` {
		t.Fatalf("deltas = %q", deltas)
	}
}

func TestAnthropicStreamRefusal(t *testing.T) {
	c := fakeAnthropic(t, cannedStream([]string{"I can't"}, "refusal", false))
	resp, err := c.Complete(context.Background(), segReq, nil)
	if !errors.Is(err, ErrRefusal) {
		t.Fatalf("err = %v, want ErrRefusal", err)
	}
	if resp.StopReason != "refusal" || resp.OutputTokens != 17 {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestAnthropicStreamMaxTokens(t *testing.T) {
	c := fakeAnthropic(t, cannedStream([]string{`{"sms":"yar`}, "max_tokens", false))
	resp, err := c.Complete(context.Background(), segReq, nil)
	if !errors.Is(err, ErrTruncated) || errors.Is(err, ErrRefusal) || !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("err = %v", err)
	}
	if resp.Text != `{"sms":"yar` {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestAnthropicStreamMidStreamErrorKeepsPartial(t *testing.T) {
	c := fakeAnthropic(t, cannedStream([]string{`{"sms":`, `"yarım`}, "", true))
	resp, err := c.Complete(context.Background(), segReq, nil)
	if err == nil {
		t.Fatal("want error")
	}
	if resp.Text != `{"sms":"yarım` || resp.InputTokens != 42 {
		t.Fatalf("resp = %+v", resp)
	}
}
