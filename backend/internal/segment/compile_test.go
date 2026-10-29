package segment_test

import (
	"strings"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/segment"
)

func TestCompileUsesParamsOnly(t *testing.T) {
	asOf := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	r := filtered(cond("city", "in", []string{"İstanbul"}), cond("abandoned_cart", "within_days", 30))
	where, args, err := segment.Compile(r, 2, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(where, "İstanbul") || strings.Contains(where, "now()") {
		t.Fatalf("literal or now() in SQL: %s", where)
	}
	if !strings.Contains(where, "$3") || strings.Contains(where, "$1") || strings.Contains(where, "$2") {
		t.Fatalf("placeholders wrong: %s", where)
	}
	found := false
	for _, a := range args {
		if ss, ok := a.([]string); ok && len(ss) == 1 && ss[0] == "İstanbul" {
			found = true
		}
	}
	if !found {
		t.Fatalf("args missing city: %v", args)
	}
}

func TestCompileAll(t *testing.T) {
	where, args, err := segment.Compile(segment.Rule{Audience: "all"}, 0, time.Now())
	if err != nil || where != "TRUE" || len(args) != 0 {
		t.Fatal(where, args, err)
	}
}

func TestCompileRejectsInvalid(t *testing.T) {
	if _, _, err := segment.Compile(filtered(cond("sql", "eq", "x")), 0, time.Now()); err == nil {
		t.Fatal("expected error")
	}
}
