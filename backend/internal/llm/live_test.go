//go:build live

package llm

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/content"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/google/uuid"
)

// TestAnthropicSmoke calls the real API. Run with:
//
//	ANTHROPIC_API_KEY=... go test ./internal/llm -run TestAnthropicSmoke -tags=live -v
func TestAnthropicSmoke(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	model := os.Getenv("ANTHROPIC_MODEL_SEGMENT")
	if model == "" {
		model = "claude-sonnet-5-5"
	}
	cl := NewAnthropic(key, model)
	l := &memLog{}
	c := &Composer{SegmentClient: cl, ContentClient: cl, Log: l, MaxRepairs: 2}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	brief := "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun."
	r, err := c.Segment(ctx, uuid.New(), brief)
	if err != nil {
		t.Fatalf("segment: %v (logs: %+v)", err, l.recs)
	}
	if _, errs := segment.Validate(r); len(errs) > 0 {
		t.Fatal(errs)
	}
	t.Logf("rule: %+v (attempts %d)", r, len(l.recs))

	partials := 0
	cc, err := c.Content(ctx, uuid.New(), brief, r, []string{"sms", "email"}, content.WorstCase{},
		func(e Event) {
			if e.Type == "partial" {
				partials++
			}
		})
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	t.Logf("content: %+v (partials %d)", cc, partials)
}
