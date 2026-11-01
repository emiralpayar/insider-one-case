package segment_test

import (
	"regexp"
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

func TestCompileIncompleteMatchesNobody(t *testing.T) {
	where, args, err := segment.Compile(segment.Rule{Audience: "filtered", Unmapped: []string{"VIP"}}, 0, time.Now())
	if err != nil || where != "FALSE" || len(args) != 0 {
		t.Fatal(where, args, err)
	}
}

// Every field's value goes in as a bind parameter: none of the (distinctive)
// values appears in the SQL text once the $n placeholders are removed.
func TestCompileAllFieldsParamsOnly(t *testing.T) {
	r := filtered(
		cond("city", "not_in", []string{"Ankara"}),
		cond("language", "eq", "en"),
		cond("abandoned_cart", "within_days", 37),
		cond("purchased", "not_within_days", 41),
		cond("order_count", "gte", 43),
		cond("total_spent", "lte", 4747),
		cond("signed_up", "within_days", 53),
	)
	where, args, err := segment.Compile(r, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	text := regexp.MustCompile(`\$\d+`).ReplaceAllString(where, "")
	for _, v := range []string{"Ankara", "'en'", "37", "41", "43", "4747", "53"} {
		if strings.Contains(text, v) {
			t.Fatalf("value %q in SQL text: %s", v, where)
		}
	}
	if len(args) < 7 {
		t.Fatalf("args = %v", args)
	}
}
