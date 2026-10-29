// Package segment defines the segment rule DSL: the safety boundary between
// LLM output and SQL. Rules are validated against a whitelist and compiled to
// parameterized SQL; no LLM-supplied string is ever placed in SQL text.
package segment

import "encoding/json"

// Rule is a segment definition. Conditions are combined with AND.
type Rule struct {
	Audience    string      `json:"audience"` // "all" | "filtered"
	Conditions  []Condition `json:"conditions"`
	Unmapped    []string    `json:"unmapped"`
	Explanation string      `json:"explanation"`
}

// Condition is a single field/op/value predicate.
type Condition struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

type valueKind int

const (
	kindCities valueKind = iota
	kindLanguage
	kindInt
)

type fieldSpec struct {
	ops      map[string]bool
	kind     valueKind
	min, max int64
}

// whitelist is the spec §6.2 table.
var whitelist = map[string]fieldSpec{
	"city":           {ops: set("in", "not_in"), kind: kindCities},
	"language":       {ops: set("eq"), kind: kindLanguage},
	"abandoned_cart": {ops: set("within_days"), kind: kindInt, min: 1, max: 365},
	"purchased":      {ops: set("within_days", "not_within_days"), kind: kindInt, min: 1, max: 365},
	"order_count":    {ops: set("gte", "lte"), kind: kindInt, min: 0, max: 1000},
	"total_spent":    {ops: set("gte", "lte"), kind: kindInt, min: 0, max: 1000000},
	"signed_up":      {ops: set("within_days"), kind: kindInt, min: 1, max: 3650},
}

func set(s ...string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}
