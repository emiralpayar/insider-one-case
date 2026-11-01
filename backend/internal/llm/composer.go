package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/content"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/emiralpayar/campaign-composer/internal/verr"
	"github.com/google/uuid"
)

// DefaultMaxRepairs is used when Composer.MaxRepairs is zero.
const DefaultMaxRepairs = 2

// logTimeout bounds a call-log write (it runs detached from request cancellation).
const logTimeout = 2 * time.Second

// emptyOutput stands in for an empty model answer in the repair history; the
// API rejects empty text blocks.
const emptyOutput = "(boş çıktı)"

// Composer runs the generate → parse → validate → repair loop on top of the
// model clients. Segment and refine use SegmentClient; content uses
// ContentClient.
type Composer struct {
	SegmentClient Client
	ContentClient Client
	Log           CallLogger // nil disables logging
	// MaxRepairs is the number of repair attempts after the first call
	// (total attempts = 1 + MaxRepairs). Zero means DefaultMaxRepairs; a
	// negative value disables repairs.
	MaxRepairs int
}

// Event is a progress notification from Content.
type Event struct {
	Type    string         `json:"type"` // "partial" | "repair"
	Partial PartialContent `json:"partial"`
	Reason  verr.List      `json:"reason,omitempty"`
}

// Segment turns a brief into a validated segment rule.
func (c *Composer) Segment(ctx context.Context, campaignID uuid.UUID, brief string) (segment.Rule, error) {
	req := Request{
		Purpose:  "segment",
		System:   segmentSystem,
		Messages: []Message{{Role: "user", Text: segmentUserPrompt(brief)}},
		Schema:   segment.JSONSchema(),
		Hint:     map[string]any{"brief": brief},
	}
	return c.rule(ctx, campaignID, req)
}

// Refine applies a natural-language instruction to an existing rule. The
// result goes through the same validation as Segment.
func (c *Composer) Refine(ctx context.Context, campaignID uuid.UUID, brief string, current segment.Rule, instruction string) (segment.Rule, error) {
	req := Request{
		Purpose:  "refine",
		System:   segmentSystem,
		Messages: []Message{{Role: "user", Text: refineUserPrompt(brief, current, instruction)}},
		Schema:   segment.JSONSchema(),
		Hint:     map[string]any{"brief": brief, "current": current, "instruction": instruction},
	}
	return c.rule(ctx, campaignID, req)
}

func (c *Composer) rule(ctx context.Context, campaignID uuid.UUID, req Request) (segment.Rule, error) {
	var out segment.Rule
	err := c.run(ctx, c.SegmentClient, campaignID, req, nil, func(text string) verr.List {
		var r segment.Rule
		if errs := decodeStrict(text, &r); errs != nil {
			return errs
		}
		norm, errs := segment.ValidateGenerated(r)
		if len(errs) == 0 {
			out = norm
		}
		return errs
	}, nil)
	return out, err
}

// Content generates message content for the given channels only (a subset of
// "sms", "email"); fields of other channels are left empty for the caller to
// merge. ev (may be nil) receives "partial" events while the output streams
// and a "repair" event before each repair attempt.
func (c *Composer) Content(ctx context.Context, campaignID uuid.UUID, brief string, rule segment.Rule,
	channels []string, wc content.WorstCase, ev func(Event)) (content.Content, error) {
	if len(channels) == 0 {
		return content.Content{}, errors.New("llm: no channels requested")
	}
	for _, ch := range channels {
		if ch != "sms" && ch != "email" {
			return content.Content{}, fmt.Errorf("llm: unknown channel %q", ch)
		}
	}
	if ev == nil {
		ev = func(Event) {}
	}
	req := Request{
		Purpose:  "content",
		System:   contentSystem(wc),
		Messages: []Message{{Role: "user", Text: contentUserPrompt(brief, rule, channels)}},
		Schema:   content.JSONSchema(channels),
		Hint:     map[string]any{"brief": brief, "rule": rule, "channels": channels},
	}

	var (
		buf  strings.Builder
		last PartialContent
		out  content.Content
	)
	onDelta := func(d string) {
		buf.WriteString(d)
		if p := ParsePartial(buf.String()); p != last {
			last = p
			ev(Event{Type: "partial", Partial: p})
		}
	}
	err := c.run(ctx, c.ContentClient, campaignID, req, onDelta, func(text string) verr.List {
		var cc content.Content
		if errs := decodeStrict(text, &cc); errs != nil {
			return errs
		}
		if !hasChannel(channels, "sms") {
			cc.SMS = ""
		}
		if !hasChannel(channels, "email") {
			cc.Email = content.Email{}
		}
		errs := content.Validate(cc, channels, wc)
		errs = append(errs, content.UnrequestedClaims(brief, cc)...)
		if len(errs) == 0 {
			out = cc
		}
		return errs
	}, func(errs verr.List) {
		buf.Reset()
		last = PartialContent{}
		ev(Event{Type: "repair", Reason: errs})
	})
	return out, err
}

// run executes up to 1+MaxRepairs attempts. check parses and validates the
// raw output and returns nil when it is acceptable. onRepair (may be nil) is
// called before each repair attempt. Client errors (transport, refusal,
// truncation) are logged and returned without repair.
func (c *Composer) run(ctx context.Context, client Client, campaignID uuid.UUID, req Request,
	onDelta func(string), check func(text string) verr.List, onRepair func(verr.List)) error {
	if client == nil {
		return fmt.Errorf("llm: no client configured for %s", req.Purpose)
	}
	repairs := c.MaxRepairs
	if repairs == 0 {
		repairs = DefaultMaxRepairs
	}
	if repairs < 0 {
		repairs = 0
	}
	var cid *uuid.UUID
	if campaignID != uuid.Nil {
		cid = &campaignID
	}
	var lastErrs verr.List
	attempts := 1 + repairs
	for attempt := 1; attempt <= attempts; attempt++ {
		start := time.Now()
		resp, err := client.Complete(ctx, req, onDelta)
		rec := CallRecord{
			CampaignID: cid, Purpose: req.Purpose, Attempt: attempt,
			Request:      loggedRequest(client, req),
			ResponseText: resp.Text, Model: resp.Model,
			InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens,
			Latency: time.Since(start),
		}
		if err != nil {
			rec.Err = err.Error()
			c.log(ctx, rec)
			return fmt.Errorf("llm: %s attempt %d: %w", req.Purpose, attempt, err)
		}
		lastErrs = check(resp.Text)
		rec.ValidationErrors = lastErrs
		c.log(ctx, rec)
		if len(lastErrs) == 0 {
			return nil
		}
		if attempt == attempts {
			break
		}
		prev := resp.Text
		if strings.TrimSpace(prev) == "" {
			prev = emptyOutput
		}
		errJSON, _ := json.Marshal(lastErrs)
		msgs := make([]Message, 0, len(req.Messages)+2)
		msgs = append(msgs, req.Messages...)
		msgs = append(msgs,
			Message{Role: "assistant", Text: prev},
			Message{Role: "user", Text: repairPrompt(errJSON)})
		req.Messages = msgs
		if onRepair != nil {
			onRepair(lastErrs)
		}
	}
	return &InvalidOutputError{Purpose: req.Purpose, Errors: lastErrs, Attempts: attempts}
}

// log writes rec; a logging failure must not break the user flow, so it is
// reported via slog only.
func (c *Composer) log(ctx context.Context, rec CallRecord) {
	if c.Log == nil {
		return
	}
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), logTimeout)
	defer cancel()
	if err := c.Log.Log(lctx, rec); err != nil {
		slog.Error("llm call log failed", "purpose", rec.Purpose, "attempt", rec.Attempt, "err", err)
	}
}

// loggedRequest is what goes into llm_calls.request. Hint is included only for
// the mock clients since real providers never see it.
func loggedRequest(client Client, req Request) map[string]any {
	m := map[string]any{
		"client":   client.Name(),
		"system":   req.System,
		"messages": req.Messages,
		"schema":   req.Schema,
	}
	if client.Name() != "anthropic" && req.Hint != nil {
		m["hint"] = req.Hint
	}
	return m
}

// decodeStrict parses model output into v. Surrounding whitespace and a
// ```json fence are tolerated; anything else (prose, trailing data, unknown
// fields) is an invalid_json validation error so that it can be repaired.
func decodeStrict(text string, v any) verr.List {
	s := stripFence(text)
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return verr.List{{Path: "", Code: "invalid_json", Message: "Çıktı geçerli JSON değil veya şemaya uymuyor: " + err.Error()}}
	}
	if strings.TrimSpace(s[dec.InputOffset():]) != "" {
		return verr.List{{Path: "", Code: "invalid_json", Message: "JSON nesnesinden sonra fazladan içerik var"}}
	}
	return nil
}

func stripFence(text string) string {
	s := strings.TrimSpace(text)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if nl := strings.IndexByte(s, '\n'); nl >= 0 && !strings.ContainsAny(s[:nl], "{[") {
			s = s[nl+1:] // drop the language tag line (e.g. "json")
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	return s
}

func hasChannel(channels []string, ch string) bool {
	for _, c := range channels {
		if c == ch {
			return true
		}
	}
	return false
}
