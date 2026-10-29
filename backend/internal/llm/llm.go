// Package llm wraps the language model behind a small interface and makes its
// output reliable: structured JSON output, backend validation, a bounded
// repair loop and a log row for every call.
package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/emiralpayar/campaign-composer/internal/verr"
)

// Client is a single-shot model call. onDelta (may be nil) receives text
// deltas as they stream in.
type Client interface {
	Complete(ctx context.Context, req Request, onDelta func(text string)) (Response, error)
	Name() string // "anthropic" | "mock" | "scripted"
}

// Request is a provider-neutral model request.
type Request struct {
	Purpose  string         `json:"purpose"` // "segment" | "refine" | "content"
	System   string         `json:"system"`
	Messages []Message      `json:"messages"`
	Schema   map[string]any `json:"schema"` // structured output JSON schema
	// Hint is structured input used only by the mock client (brief,
	// instruction, current rule, channels). Real clients ignore it.
	Hint map[string]any `json:"hint,omitempty"`
}

// Message is one conversation turn.
type Message struct {
	Role string `json:"role"` // "user" | "assistant"
	Text string `json:"text"`
}

// Response is the raw model output.
type Response struct {
	Text                      string
	Model                     string
	InputTokens, OutputTokens int
	StopReason                string
}

// ErrRefusal is returned when the model refuses to answer.
var ErrRefusal = errors.New("llm: model refused the request")

// ErrTruncated is returned when the output hit the max_tokens ceiling (the
// JSON is cut off).
var ErrTruncated = errors.New("llm: output truncated at max_tokens")

// InvalidOutputError means the model output still failed validation after all
// repair attempts.
type InvalidOutputError struct {
	Purpose  string
	Errors   verr.List
	Attempts int
}

func (e *InvalidOutputError) Error() string {
	return fmt.Sprintf("llm: %s output invalid after %d attempts: %v", e.Purpose, e.Attempts, e.Errors)
}
