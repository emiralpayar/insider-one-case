package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Output token ceilings. Adaptive thinking tokens count against max_tokens,
// so these leave headroom above the JSON itself.
const (
	segmentMaxTokens = 8192
	contentMaxTokens = 16384
)

type anthropicClient struct {
	client anthropic.Client
	model  string
}

// NewAnthropic returns a Client backed by the Anthropic Messages API. Create
// one per model (segment and content may use different models).
func NewAnthropic(apiKey, model string) Client {
	return &anthropicClient{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
	}
}

func (a *anthropicClient) Name() string { return "anthropic" }

// Complete streams the response, forwarding text deltas to onDelta. A refusal
// returns ErrRefusal; hitting max_tokens is an error (the JSON is truncated).
func (a *anthropicClient) Complete(ctx context.Context, req Request, onDelta func(string)) (Response, error) {
	stream := a.client.Messages.NewStreaming(ctx, buildParams(a.model, req))
	defer stream.Close()
	msg := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			return a.response(&msg), fmt.Errorf("llm: anthropic: accumulate: %w", err)
		}
		if onDelta == nil {
			continue
		}
		if ev, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			if d, ok := ev.Delta.AsAny().(anthropic.TextDelta); ok {
				onDelta(d.Text)
			}
		}
	}
	resp := a.response(&msg)
	if err := stream.Err(); err != nil {
		// Return what arrived so the call log row is not empty.
		return resp, fmt.Errorf("llm: anthropic: %w", err)
	}
	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		return resp, fmt.Errorf("%w (category %q)", ErrRefusal, msg.StopDetails.Category)
	case anthropic.StopReasonMaxTokens:
		return resp, fmt.Errorf("llm: anthropic: output truncated at max_tokens")
	}
	return resp, nil
}

// response converts a (possibly partially) accumulated message.
func (a *anthropicClient) response(msg *anthropic.Message) Response {
	var text strings.Builder
	for _, block := range msg.Content {
		// Read the union fields directly: AsAny() decodes the raw JSON, which
		// Accumulate refreshes only at content_block_stop.
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	resp := Response{
		Text:         text.String(),
		Model:        msg.Model,
		InputTokens:  int(msg.Usage.InputTokens),
		OutputTokens: int(msg.Usage.OutputTokens),
		StopReason:   string(msg.StopReason),
	}
	if resp.Model == "" {
		resp.Model = a.model
	}
	return resp
}

// buildParams maps a Request to Messages API params: structured output via
// output_config.format (json_schema), effort per purpose, adaptive thinking.
// No temperature/top_p, no forced tool_choice. Request.Hint is not sent.
func buildParams(model string, req Request) anthropic.MessageNewParams {
	effort := anthropic.OutputConfigEffortLow
	maxTokens := int64(segmentMaxTokens)
	if req.Purpose == "content" {
		effort = anthropic.OutputConfigEffortMedium
		maxTokens = contentMaxTokens
	}
	msgs := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, m := range req.Messages {
		block := anthropic.NewTextBlock(m.Text)
		if m.Role == "assistant" {
			msgs = append(msgs, anthropic.NewAssistantMessage(block))
		} else {
			msgs = append(msgs, anthropic.NewUserMessage(block))
		}
	}
	p := anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  msgs,
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: effort,
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema},
		},
	}
	if req.System != "" {
		p.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	return p
}
