package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/content"
	"github.com/emiralpayar/campaign-composer/internal/llm"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/emiralpayar/campaign-composer/internal/verr"
)

// doneEvent is the payload of the final "done" SSE event.
type doneEvent struct {
	Content        content.Content  `json:"content"`
	Issues         verr.List        `json:"issues"`
	Saved          bool             `json:"saved"`
	SMSStats       content.SMSStats `json:"sms_stats"`
	RenderedSample renderedSample   `json:"rendered_sample"`
}

// renderedSample is the content rendered for a real audience member and for
// the same member without a first name.
type renderedSample struct {
	Named    content.Content `json:"named"`
	Nameless content.Content `json:"nameless"`
}

// contentStream generates content over SSE.
//
//	GET /api/campaigns/{id}/content/stream?channel=all|sms|email
//
// Events: partial (llm.PartialContent), repair (verr.List), done (doneEvent),
// error ({code,message,details}). channel=sms|email regenerates only that
// channel and keeps the other one from the saved content. The merged content
// is saved only when it passes validation. Errors before the stream opens are
// plain JSON responses. A client disconnect cancels the request context,
// which stops the model call; nothing is saved then.
func (s *Server) contentStream(w http.ResponseWriter, r *http.Request) {
	if !s.requireLLM(w, r) {
		return
	}
	c, ok := s.loadCampaign(w, r)
	if !ok {
		return
	}
	var gen []string
	switch ch := r.URL.Query().Get("channel"); ch {
	case "", "all":
		gen = c.Channels
	case "sms", "email":
		gen = []string{ch}
	default:
		writeError(w, r, badRequest(fmt.Sprintf("Geçersiz kanal: %q (all, sms, email)", ch)))
		return
	}
	switch {
	case campaign.Locked(c.Status):
		writeError(w, r, campaign.ErrLocked)
		return
	case c.Rule == nil:
		writeError(w, r, campaign.ErrNotReady)
		return
	}
	ctx := r.Context()
	asOf := s.now()
	wc, err := campaign.WorstCaseFor(ctx, s.pool, *c.Rule, asOf)
	if err != nil {
		writeError(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, fmt.Errorf("streaming unsupported"))
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Writes come from the LLM callback and the heartbeat goroutine.
	var mu sync.Mutex
	write := func(frame string) {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		io.WriteString(w, frame)
		flusher.Flush()
	}
	send := func(event string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		write(fmt.Sprintf("event: %s\ndata: %s\n\n", event, b))
	}
	sendErr := func(err error) { send("error", toAPIError(err)) }

	// Heartbeat comments keep proxies from closing an idle stream while the
	// model is thinking.
	stopBeat := make(chan struct{})
	beatDone := make(chan struct{})
	go func() {
		defer close(beatDone)
		t := time.NewTicker(s.heartbeat)
		defer t.Stop()
		for {
			select {
			case <-stopBeat:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				write(": ping\n\n")
			}
		}
	}()
	out, err := s.composer.Content(ctx, c.ID, c.Brief, *c.Rule, gen, wc, func(e llm.Event) {
		switch e.Type {
		case "partial":
			send("partial", e.Partial)
		case "repair":
			send("repair", e.Reason)
		}
	})
	close(stopBeat)
	<-beatDone
	if ctx.Err() != nil {
		return // client went away: no save, no events
	}
	if err != nil {
		sendErr(llmFailure(err))
		return
	}

	merged := content.Content{}
	if c.Content != nil {
		merged = *c.Content
	}
	for _, ch := range gen {
		switch ch {
		case "sms":
			merged.SMS = out.SMS
		case "email":
			merged.Email = out.Email
		}
	}
	done := doneEvent{Content: merged, Issues: verr.List{}, SMSStats: smsStats(merged, wc)}
	// Everything that can fail happens before the save, so a saved result is
	// always followed by "done" (never by "error").
	if done.RenderedSample, err = s.renderSample(r, *c.Rule, merged, wc); err != nil {
		sendErr(err)
		return
	}
	if issues := content.Validate(merged, c.Channels, wc); len(issues) > 0 {
		done.Issues = issues
	} else {
		if ctx.Err() != nil {
			return
		}
		stored, err := s.store.SaveContentChannels(ctx, c.ID, merged, gen)
		if err != nil {
			sendErr(err)
			return
		}
		// The other channel may have been edited while this one streamed.
		done.Content = stored
		done.SMSStats = smsStats(stored, wc)
		done.Saved = true
	}
	send("done", done)
}

// renderSample renders c for the first named preview sample user (the first
// sample user if none has a name; worst-case values when the audience is
// empty) and for the same user without a name.
func (s *Server) renderSample(r *http.Request, rule segment.Rule, c content.Content, wc content.WorstCase) (renderedSample, error) {
	pv, err := segment.Preview(r.Context(), s.pool, rule, s.now())
	if err != nil {
		return renderedSample{}, err
	}
	wc = wc.WithDefaults()
	vars := map[string]string{"first_name": wc.LongestFirstName, "city": wc.LongestCity}
	for i, u := range pv.Sample {
		// Prefer a sample user with a name so that "named" differs from "nameless".
		if i == 0 || u.FirstName != "" {
			vars = map[string]string{"first_name": u.FirstName, "city": u.City}
		}
		if u.FirstName != "" {
			break
		}
	}
	nameless := map[string]string{"first_name": "", "city": vars["city"]}
	return renderedSample{Named: renderContent(c, vars), Nameless: renderContent(c, nameless)}, nil
}

func renderContent(c content.Content, vars map[string]string) content.Content {
	return content.Content{
		SMS: content.Render(c.SMS, vars),
		Email: content.Email{
			Subject:   content.Render(c.Email.Subject, vars),
			Preheader: content.Render(c.Email.Preheader, vars),
			Body:      content.Render(c.Email.Body, vars),
		},
	}
}
