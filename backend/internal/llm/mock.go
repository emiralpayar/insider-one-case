package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/emiralpayar/campaign-composer/internal/segment"
)

// mockClient is a deterministic, keyword-based Client for tests and key-less
// development. It reads Request.Hint only and never sees the prompts. It is
// only ever constructed explicitly (LLM_PROVIDER=mock or tests).
type mockClient struct{}

// NewMock returns the deterministic mock Client.
func NewMock() Client { return mockClient{} }

func (mockClient) Name() string { return "mock" }

func (mockClient) Complete(ctx context.Context, req Request, onDelta func(string)) (Response, error) {
	var v any
	switch req.Purpose {
	case "segment":
		v = mockSegment(hintString(req.Hint, "brief"))
	case "refine":
		cur, err := hintRule(req.Hint)
		if err != nil {
			return Response{}, err
		}
		v = mockRefine(cur, hintString(req.Hint, "instruction"))
	case "content":
		v = mockContent(hintString(req.Hint, "brief"), hintStrings(req.Hint, "channels"))
	case "similarity":
		v = mockSimilarity(hintString(req.Hint, "brief"), hintCandidates(req.Hint))
	default:
		return Response{}, fmt.Errorf("llm: mock: unknown purpose %q", req.Purpose)
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return Response{}, err
	}
	text := strings.TrimSpace(b.String())
	if err := streamChunks(ctx, text, onDelta); err != nil {
		return Response{}, err
	}
	return Response{Text: text, Model: "mock", StopReason: "end_turn"}, nil
}

// fold lower-cases with Turkish rules and folds ı to i (comparison only).
func fold(s string) string {
	return strings.ReplaceAll(strings.Map(unicode.TurkishCase.ToLower, s), "ı", "i")
}

// wordIndex returns the byte index of the first occurrence of word in s that
// starts at a word boundary, or -1. Both must already be folded.
func wordIndex(s, word string) int {
	for off := 0; ; {
		i := strings.Index(s[off:], word)
		if i < 0 {
			return -1
		}
		i += off
		if i == 0 || !isLetterBefore(s, i) {
			return i
		}
		off = i + len(word)
	}
}

func isLetterBefore(s string, i int) bool {
	r := []rune(s[:i])
	return unicode.IsLetter(r[len(r)-1])
}

// citiesIn returns the known cities mentioned in text, in order of appearance.
func citiesIn(text string) []string {
	f := fold(text)
	type hit struct {
		pos  int
		city string
	}
	var hits []hit
	for _, c := range segment.Cities {
		if i := wordIndex(f, fold(c)); i >= 0 {
			hits = append(hits, hit{i, c})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].pos < hits[b].pos })
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.city
	}
	return out
}

var (
	daysRe      = regexp.MustCompile(`son (\d+) gün`)
	allRe       = regexp.MustCompile(fold(`herkes|tüm kullanıcılar`))
	purchasedRe = regexp.MustCompile(fold(`alışveriş yapmış|satın almış`))
	// unmappedWords are audience qualifiers with no matching field.
	unmappedWords = []string{"VIP", "sadık", "premium"}
)

func mockSegment(brief string) segment.Rule {
	f := fold(brief)
	r := segment.Rule{Audience: "filtered", Conditions: []segment.Condition{}, Unmapped: []string{}}
	for _, w := range unmappedWords {
		if wordIndex(f, fold(w)) >= 0 {
			r.Unmapped = append(r.Unmapped, w)
		}
	}
	days := 30
	if m := daysRe.FindStringSubmatch(f); m != nil {
		fmt.Sscan(m[1], &days)
	}
	var parts []string
	if cs := citiesIn(brief); len(cs) > 0 {
		r.Conditions = append(r.Conditions, cityCond(cs))
		parts = append(parts, strings.Join(cs, "/")+" şehrinde yaşayan")
	}
	if strings.Contains(f, "sepet") {
		r.Conditions = append(r.Conditions, segment.Condition{Field: "abandoned_cart", Op: "within_days", Value: rawJSON(days)})
		parts = append(parts, fmt.Sprintf("son %d günde sepete ürün ekleyip almamış", days))
	}
	if purchasedRe.MatchString(f) {
		r.Conditions = append(r.Conditions, segment.Condition{Field: "purchased", Op: "within_days", Value: rawJSON(days)})
		parts = append(parts, fmt.Sprintf("son %d günde alışveriş yapmış", days))
	}
	// "herkes" means everyone only when nothing narrows the audience
	// ("İstanbul'daki herkese" is still a city filter).
	if len(r.Conditions) == 0 && allRe.MatchString(f) {
		r.Audience = "all"
		r.Explanation = "Tüm kullanıcılar (mock)"
		return r
	}
	if len(parts) == 0 {
		r.Explanation = "Brief'ten tanınan bir kitle koşulu çıkarılamadı (mock)"
		return r
	}
	r.Explanation = strings.Join(parts, ", ") + " kullanıcılar (mock)"
	return r
}

func mockRefine(cur segment.Rule, instruction string) segment.Rule {
	r := cur
	r.Conditions = append([]segment.Condition(nil), cur.Conditions...)
	if r.Unmapped == nil {
		r.Unmapped = []string{}
	}
	add := citiesIn(instruction)
	if len(add) == 0 {
		return r
	}
	remove := strings.Contains(fold(instruction), fold("çıkar"))
	idx := -1
	for i, c := range r.Conditions {
		if c.Field == "city" && c.Op == "in" {
			idx = i
		}
	}
	var cities []string
	if idx >= 0 {
		_ = json.Unmarshal(r.Conditions[idx].Value, &cities)
	}
	for _, c := range add {
		if remove {
			cities = removeString(cities, c)
		} else if !containsString(cities, c) {
			cities = append(cities, c)
		}
	}
	switch {
	case idx >= 0 && len(cities) == 0:
		r.Conditions = append(r.Conditions[:idx], r.Conditions[idx+1:]...)
	case idx >= 0:
		r.Conditions[idx] = cityCond(cities)
	case len(cities) > 0:
		r.Audience = "filtered"
		r.Conditions = append(r.Conditions, cityCond(cities))
	}
	r.Explanation = cur.Explanation + " — düzenlendi: " + instruction + " (mock)"
	return r
}

type mockContentOut struct {
	SMS   string        `json:"sms,omitempty"`
	Email *mockEmailOut `json:"email,omitempty"`
}

type mockEmailOut struct {
	Subject   string `json:"subject"`
	Preheader string `json:"preheader"`
	Body      string `json:"body"`
}

var discountRe = regexp.MustCompile(`%\s?\d+`)

func mockContent(brief string, channels []string) mockContentOut {
	offer := "sana özel bir fırsat"
	if d := discountRe.FindString(brief); d != "" {
		offer = "sana özel " + strings.ReplaceAll(d, " ", "") + " indirim"
	}
	var out mockContentOut
	if containsString(channels, "sms") {
		out.SMS = "Merhaba {{first_name}}, " + offer + " seni bekliyor! Fırsatı kaçırma, hemen alışverişe dön."
	}
	if containsString(channels, "email") {
		out.Email = &mockEmailOut{
			Subject:   "Sepetin seni bekliyor: " + offer,
			Preheader: "Kampanya kısa süreliğine geçerli.",
			Body: "Merhaba {{first_name}},\n\n{{city}} için " + offer + " hazırladık. " +
				"Sepetindeki ürünler hâlâ seni bekliyor.\n\nHemen alışverişe dön!",
		}
	}
	return out
}

func cityCond(cities []string) segment.Condition {
	return segment.Condition{Field: "city", Op: "in", Value: rawJSON(cities)}
}

func rawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func hintString(h map[string]any, k string) string {
	s, _ := h[k].(string)
	return s
}

func hintStrings(h map[string]any, k string) []string {
	switch v := h[k].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func hintRule(h map[string]any) (segment.Rule, error) {
	if r, ok := h["current"].(segment.Rule); ok {
		return r, nil
	}
	var r segment.Rule
	b, err := json.Marshal(h["current"])
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("llm: mock: bad current rule hint: %w", err)
	}
	return r, nil
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func removeString(ss []string, s string) []string {
	out := ss[:0:0]
	for _, x := range ss {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}
