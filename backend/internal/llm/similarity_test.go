package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSimilarRepairsInvalidVerdicts(t *testing.T) {
	cands := []SimilarityCandidate{{ID: "c1", Brief: "a"}, {ID: "c2", Brief: "b"}}
	s := NewScripted(
		`{"matches":[{"id":"c1","similar":true,"reason":""},{"id":"c9","similar":false,"reason":"x"}]}`,
		`{"matches":[{"id":"c1","similar":true,"reason":"Aynı sepet hatırlatması."},{"id":"c2","similar":false,"reason":"Farklı teklif."}]}`)
	ms, err := newComposer(s, &memLog{}).Similar(context.Background(), uuid.New(), "brief", cands)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || !ms[0].Similar || ms[0].Reason == "" || ms[1].Similar {
		t.Fatalf("matches = %+v", ms)
	}
	repair := lastUser(s.Calls()[1])
	for _, code := range []string{"empty_reason", "unknown_id", "missing_id"} {
		if !strings.Contains(repair, code) {
			t.Fatalf("repair message lacks %s: %q", code, repair)
		}
	}
}

func TestMockSimilarity(t *testing.T) {
	c := &Composer{SegmentClient: NewMock()}
	ms, err := c.Similar(context.Background(), uuid.Nil, "İstanbul'da sepetini terk edenlere %30 indirim hatırlatması",
		[]SimilarityCandidate{
			{ID: "c1", Brief: "Sepetini terk eden İstanbul kullanıcılarına %30 indirim hatırlatması gönder"},
			{ID: "c2", Brief: "Ankara'daki yeni üyelere hoş geldin mesajı"},
		})
	if err != nil {
		t.Fatal(err)
	}
	if !ms[0].Similar || !strings.Contains(ms[0].Reason, "hatırlatması") || ms[1].Similar {
		t.Fatalf("matches = %+v", ms)
	}
}
