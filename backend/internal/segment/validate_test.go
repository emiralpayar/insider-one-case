package segment_test

import (
	"encoding/json"
	"testing"

	"github.com/emiralpayar/campaign-composer/internal/segment"
)

func cond(field, op string, v any) segment.Condition {
	b, _ := json.Marshal(v)
	return segment.Condition{Field: field, Op: op, Value: b}
}

func filtered(cs ...segment.Condition) segment.Rule {
	return segment.Rule{Audience: "filtered", Conditions: cs}
}

func TestValidateValid(t *testing.T) {
	r := filtered(cond("city", "in", []string{"istanbul"}), cond("abandoned_cart", "within_days", 30))
	out, errs := segment.Validate(r)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	var cities []string
	_ = json.Unmarshal(out.Conditions[0].Value, &cities)
	if len(cities) != 1 || cities[0] != "İstanbul" {
		t.Fatalf("not normalized: %v", cities)
	}
	if _, errs := segment.Validate(segment.Rule{Audience: "all"}); len(errs) != 0 {
		t.Fatalf("all: %v", errs)
	}
}

func TestValidateRejectsUnknownFieldAndRawSQL(t *testing.T) {
	_, errs := segment.Validate(filtered(cond("sql", "eq", "DROP TABLE users")))
	if len(errs) != 1 || errs[0].Code != "unknown_field" || errs[0].Path != "conditions[0].field" {
		t.Fatalf("got %v", errs)
	}
	_, errs = segment.Validate(filtered(cond("city", "in", []string{"İstanbul'; DROP TABLE users;--"})))
	if len(errs) != 1 || errs[0].Code != "unknown_city" || errs[0].Path != "conditions[0].value" {
		t.Fatalf("got %v", errs)
	}
}

func TestNormalizeCity(t *testing.T) {
	for _, in := range []string{"istanbul", "ISTANBUL", "Istanbul", "İSTANBUL"} {
		if got, ok := segment.NormalizeCity(in); !ok || got != "İstanbul" {
			t.Errorf("%q -> %q %v", in, got, ok)
		}
	}
	if got, ok := segment.NormalizeCity("izmir"); !ok || got != "İzmir" {
		t.Errorf("izmir -> %q %v", got, ok)
	}
	if got, ok := segment.NormalizeCity("eskişehir"); !ok || got != "Eskişehir" {
		t.Errorf("eskişehir -> %q %v", got, ok)
	}
	if _, ok := segment.NormalizeCity("Istanbull"); ok {
		t.Error("Istanbull accepted")
	}
}

func TestValidateCodes(t *testing.T) {
	tests := []struct {
		name string
		rule segment.Rule
		code string
		path string
	}{
		{"bad audience", segment.Rule{Audience: "x"}, "invalid_audience", "audience"},
		{"empty", filtered(), "empty_conditions", "conditions"},
		{"all+cond", segment.Rule{Audience: "all", Conditions: []segment.Condition{cond("language", "eq", "tr")}}, "all_with_conditions", "conditions"},
		{"bad op", filtered(cond("language", "in", "tr")), "invalid_op", "conditions[0].op"},
		{"lang value", filtered(cond("language", "eq", "de")), "invalid_value", "conditions[0].value"},
		{"lang type", filtered(cond("language", "eq", 1)), "invalid_value", "conditions[0].value"},
		{"city type", filtered(cond("city", "in", "İstanbul")), "invalid_value", "conditions[0].value"},
		{"city empty", filtered(cond("city", "in", []string{})), "invalid_value", "conditions[0].value"},
		{"float", filtered(cond("abandoned_cart", "within_days", 3.5)), "invalid_value", "conditions[0].value"},
		{"string num", filtered(cond("abandoned_cart", "within_days", "30")), "invalid_value", "conditions[0].value"},
		{"cart 0", filtered(cond("abandoned_cart", "within_days", 0)), "out_of_range", "conditions[0].value"},
		{"cart 366", filtered(cond("abandoned_cart", "within_days", 366)), "out_of_range", "conditions[0].value"},
		{"purchased 366", filtered(cond("purchased", "not_within_days", 366)), "out_of_range", "conditions[0].value"},
		{"order 1001", filtered(cond("order_count", "gte", 1001)), "out_of_range", "conditions[0].value"},
		{"order -1", filtered(cond("order_count", "lte", -1)), "out_of_range", "conditions[0].value"},
		{"spent big", filtered(cond("total_spent", "gte", 1000001)), "out_of_range", "conditions[0].value"},
		{"signup 3651", filtered(cond("signed_up", "within_days", 3651)), "out_of_range", "conditions[0].value"},
		{"conflict purchased", filtered(cond("purchased", "within_days", 30), cond("purchased", "not_within_days", 30)), "conflicting_conditions", "conditions[1]"},
		{"conflict city", filtered(cond("city", "in", []string{"Ankara"}), cond("city", "not_in", []string{"ankara"})), "conflicting_conditions", "conditions[1]"},
		{"conflict lang", filtered(cond("language", "eq", "tr"), cond("language", "eq", "en")), "conflicting_conditions", "conditions[1]"},
		{"conflict order", filtered(cond("order_count", "gte", 10), cond("order_count", "lte", 5)), "conflicting_conditions", "conditions[1]"},
		{"conflict order rev", filtered(cond("order_count", "lte", 5), cond("order_count", "gte", 10)), "conflicting_conditions", "conditions[1]"},
		{"conflict spent", filtered(cond("total_spent", "gte", 100), cond("total_spent", "lte", 50)), "conflicting_conditions", "conditions[1]"},
		{"conflict city in/in", filtered(cond("city", "in", []string{"Ankara"}), cond("city", "in", []string{"izmir", "Bursa"})), "conflicting_conditions", "conditions[1]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := segment.Validate(tc.rule)
			if len(errs) != 1 || errs[0].Code != tc.code || errs[0].Path != tc.path {
				t.Fatalf("want %s at %s, got %v", tc.code, tc.path, errs)
			}
		})
	}
}

func TestValidateBoundariesOK(t *testing.T) {
	r := filtered(cond("abandoned_cart", "within_days", 365), cond("order_count", "gte", 0),
		cond("total_spent", "lte", 1000000), cond("signed_up", "within_days", 3650), cond("language", "eq", "en"))
	if _, errs := segment.Validate(r); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestValidateErrorsSortedByIndex(t *testing.T) {
	_, errs := segment.Validate(filtered(cond("sql", "eq", 1), cond("order_count", "gte", 5000), cond("zzz", "eq", 1)))
	if len(errs) != 3 || errs[0].Path != "conditions[0].field" || errs[1].Path != "conditions[1].value" || errs[2].Path != "conditions[2].field" {
		t.Fatalf("got %v", errs)
	}
}

func TestSchemaSubset(t *testing.T) {
	s := segment.JSONSchema()
	if s["type"] != "object" || s["additionalProperties"] != false {
		t.Fatalf("bad schema: %v", s)
	}
	b, _ := json.Marshal(s)
	for _, bad := range []string{"minimum", "maximum", "pattern", "format"} {
		if json.Valid(b) && contains(string(b), `"`+bad+`"`) {
			t.Errorf("unsupported keyword %s", bad)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestValidateNonConflicting(t *testing.T) {
	rules := map[string]segment.Rule{
		"order range":  filtered(cond("order_count", "gte", 5), cond("order_count", "lte", 10)),
		"order equal":  filtered(cond("order_count", "gte", 5), cond("order_count", "lte", 5)),
		"spent range":  filtered(cond("total_spent", "gte", 5), cond("total_spent", "lte", 10)),
		"same lang":    filtered(cond("language", "eq", "tr"), cond("language", "eq", "tr")),
		"city overlap": filtered(cond("city", "in", []string{"Ankara", "Bursa"}), cond("city", "in", []string{"bursa"})),
	}
	for n, r := range rules {
		if _, errs := segment.Validate(r); len(errs) != 0 {
			t.Errorf("%s: %v", n, errs)
		}
	}
}

// A brief whose whole audience is unknown ("VIP") yields an incomplete rule:
// filtered, no conditions, the phrase in unmapped. It is valid as a draft and
// matches nobody until the marketer adds a condition.
func TestValidateIncompleteRule(t *testing.T) {
	r := segment.Rule{Audience: "filtered", Unmapped: []string{"VIP müşteriler"}}
	if _, errs := segment.Validate(r); len(errs) != 0 {
		t.Fatalf("incomplete rule should be valid: %v", errs)
	}
	if _, errs := segment.Validate(filtered()); len(errs) != 1 || errs[0].Code != "empty_conditions" {
		t.Fatalf("empty rule without unmapped: %v", errs)
	}
}

// The model must not paper over an unknown audience with a rule that selects
// everyone.
func TestValidateGenerated(t *testing.T) {
	cases := []struct {
		r    segment.Rule
		code string
	}{
		{segment.Rule{Audience: "all", Unmapped: []string{"VIP"}}, "all_with_unmapped"},
		{filtered(cond("order_count", "gte", 0)), "tautological_condition"},
		{filtered(cond("total_spent", "gte", 0)), "tautological_condition"},
		{filtered(cond("order_count", "lte", 1000)), "tautological_condition"},
		{filtered(cond("total_spent", "lte", 1000000)), "tautological_condition"},
	}
	for _, c := range cases {
		_, errs := segment.ValidateGenerated(c.r)
		found := false
		for _, e := range errs {
			found = found || e.Code == c.code
		}
		if !found {
			t.Errorf("%+v: want %s, got %v", c.r, c.code, errs)
		}
	}
	ok := []segment.Rule{
		{Audience: "all"},
		{Audience: "filtered", Unmapped: []string{"VIP"}},
		filtered(cond("order_count", "gte", 1)),
		filtered(cond("order_count", "lte", 999)),
	}
	for _, r := range ok {
		if _, errs := segment.ValidateGenerated(r); len(errs) != 0 {
			t.Errorf("%+v: %v", r, errs)
		}
	}
}
