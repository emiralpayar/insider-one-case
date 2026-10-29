package llm

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/emiralpayar/campaign-composer/internal/content"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/google/uuid"
)

func mockRule(t *testing.T, req Request) segment.Rule {
	t.Helper()
	resp, err := NewMock().Complete(context.Background(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	var r segment.Rule
	if err := json.Unmarshal([]byte(resp.Text), &r); err != nil {
		t.Fatalf("mock output %q: %v", resp.Text, err)
	}
	return r
}

func cond(r segment.Rule, field string) *segment.Condition {
	for i := range r.Conditions {
		if r.Conditions[i].Field == field {
			return &r.Conditions[i]
		}
	}
	return nil
}

func TestMockSampleBrief(t *testing.T) {
	brief := "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun."
	r := mockRule(t, Request{Purpose: "segment", Hint: map[string]any{"brief": brief}})
	if _, errs := segment.Validate(r); len(errs) > 0 {
		t.Fatalf("mock rule invalid: %v (%+v)", errs, r)
	}
	c := cond(r, "city")
	if c == nil || c.Op != "in" || string(c.Value) != `["İstanbul"]` {
		t.Fatalf("city = %+v", c)
	}
	a := cond(r, "abandoned_cart")
	if a == nil || a.Op != "within_days" || string(a.Value) != "30" {
		t.Fatalf("abandoned_cart = %+v", a)
	}
	if cond(r, "purchased") != nil {
		t.Fatal("'alışveriş yapmayan' must not map to purchased")
	}
	if r.Explanation == "" {
		t.Fatal("empty explanation")
	}
}

func TestMockDaysAndPurchased(t *testing.T) {
	r := mockRule(t, Request{Purpose: "segment", Hint: map[string]any{"brief": "son 7 gün içinde satın almış ANKARA kullanıcıları"}})
	p := cond(r, "purchased")
	if p == nil || p.Op != "within_days" || string(p.Value) != "7" {
		t.Fatalf("purchased = %+v", p)
	}
	if c := cond(r, "city"); c == nil || string(c.Value) != `["Ankara"]` {
		t.Fatalf("city = %+v", c)
	}
}

func TestMockNoFalseCityMatch(t *testing.T) {
	// "olmuş" contains "muş" (Muş) but not at a word start.
	r := mockRule(t, Request{Purpose: "segment", Hint: map[string]any{"brief": "sepete ürün eklemiş olmuş kullanıcılar"}})
	if c := cond(r, "city"); c != nil {
		t.Fatalf("unexpected city %s", c.Value)
	}
}

func TestMockAll(t *testing.T) {
	r := mockRule(t, Request{Purpose: "segment", Hint: map[string]any{"brief": "herkese gönder"}})
	if r.Audience != "all" || len(r.Conditions) != 0 {
		t.Fatalf("rule = %+v", r)
	}
	if _, errs := segment.Validate(r); len(errs) > 0 {
		t.Fatal(errs)
	}
}

func TestMockUnmapped(t *testing.T) {
	r := mockRule(t, Request{Purpose: "segment", Hint: map[string]any{"brief": "VIP müşterilere"}})
	if !slices.Contains(r.Unmapped, "VIP") {
		t.Fatalf("unmapped = %v", r.Unmapped)
	}
}

func TestMockRefineAddCity(t *testing.T) {
	cur := segment.Rule{Audience: "filtered", Conditions: []segment.Condition{
		{Field: "city", Op: "in", Value: json.RawMessage(`["İstanbul"]`)},
		{Field: "abandoned_cart", Op: "within_days", Value: json.RawMessage(`30`)},
	}, Unmapped: []string{}}
	r := mockRule(t, Request{Purpose: "refine", Hint: map[string]any{"current": cur, "instruction": "Ankara'yı da ekle"}})
	var cities []string
	c := cond(r, "city")
	if c == nil {
		t.Fatalf("rule = %+v", r)
	}
	if err := json.Unmarshal(c.Value, &cities); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cities, []string{"İstanbul", "Ankara"}) {
		t.Fatalf("cities = %v", cities)
	}
	if cond(r, "abandoned_cart") == nil {
		t.Fatal("refine dropped existing condition")
	}
}

func TestMockContent(t *testing.T) {
	var deltas []string
	resp, err := NewMock().Complete(context.Background(), Request{Purpose: "content",
		Hint: map[string]any{"brief": "x", "channels": []string{"sms", "email"}}}, func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(deltas, "") != resp.Text || len(deltas) < 2 {
		t.Fatalf("deltas do not reconstruct text")
	}
	for _, d := range deltas[:len(deltas)-1] {
		if utf8.RuneCountInString(d) != 8 {
			t.Fatalf("delta %q is not 8 runes", d)
		}
	}
	var c content.Content
	if err := json.Unmarshal([]byte(resp.Text), &c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.SMS, "{{first_name}}") {
		t.Fatalf("sms = %q", c.SMS)
	}
	if errs := content.Validate(c, []string{"sms", "email"}, content.WorstCase{}); len(errs) > 0 {
		t.Fatal(errs)
	}
}

func TestMockThroughComposer(t *testing.T) {
	m := NewMock()
	c := &Composer{SegmentClient: m, ContentClient: m, Log: &memLog{}, MaxRepairs: 2}
	ctx := context.Background()
	r, err := c.Segment(ctx, uuid.New(), "İzmir'de son 14 günde sepetini terk eden kullanıcılar")
	if err != nil {
		t.Fatal(err)
	}
	r, err = c.Refine(ctx, uuid.New(), "brief", r, "Bursa'yı da ekle")
	if err != nil {
		t.Fatal(err)
	}
	if v := string(cond(r, "city").Value); v != `["İzmir","Bursa"]` {
		t.Fatalf("cities = %s", v)
	}
	cc, err := c.Content(ctx, uuid.New(), "brief", r, []string{"sms"}, content.WorstCase{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cc.SMS == "" || cc.Email.Subject != "" {
		t.Fatalf("sms-only content = %+v", cc)
	}
}

func TestMockAllWordWithConditionsStaysFiltered(t *testing.T) {
	r := mockRule(t, Request{Purpose: "segment", Hint: map[string]any{"brief": "İstanbul'daki herkese %20 indirim gönder"}})
	if r.Audience != "filtered" {
		t.Fatalf("audience = %q, want filtered (%+v)", r.Audience, r)
	}
	if c := cond(r, "city"); c == nil || c.Op != "in" || string(c.Value) != `["İstanbul"]` {
		t.Fatalf("city = %+v", c)
	}
	if _, errs := segment.Validate(r); len(errs) > 0 {
		t.Fatal(errs)
	}
}
