package segment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/emiralpayar/campaign-composer/internal/verr"
)

type parsed struct {
	ok     bool
	cities []string
	n      int64
	str    string
}

// Validate checks r against the whitelist and returns a normalized copy
// (canonical city names) plus errors sorted by condition index. The returned
// rule is best-effort when errors exist.
func Validate(r Rule) (Rule, verr.List) {
	out := r
	var errs verr.List
	add := func(path, code, msg string) { errs = append(errs, verr.Error{Path: path, Code: code, Message: msg}) }

	switch r.Audience {
	case "all":
		if len(r.Conditions) > 0 {
			add("conditions", "all_with_conditions", "audience \"all\" must have no conditions")
		}
	case "filtered":
		// No conditions is allowed only as an incomplete draft: the audience
		// could not be mapped (unmapped is set) and the marketer adds the
		// condition by hand. Compile matches nobody for it.
		if len(r.Conditions) == 0 && len(r.Unmapped) == 0 {
			add("conditions", "empty_conditions", "audience \"filtered\" requires at least one condition")
		}
	default:
		add("audience", "invalid_audience", fmt.Sprintf("audience must be \"all\" or \"filtered\", got %q", r.Audience))
	}

	out.Conditions = make([]Condition, len(r.Conditions))
	copy(out.Conditions, r.Conditions)
	if r.Unmapped == nil {
		out.Unmapped = []string{}
	}

	ps := make([]parsed, len(r.Conditions))

	for i, c := range r.Conditions {
		p := func(s string) string { return fmt.Sprintf("conditions[%d].%s", i, s) }
		spec, ok := whitelist[c.Field]
		if !ok {
			add(p("field"), "unknown_field", fmt.Sprintf("field %q is not allowed", c.Field))
			continue
		}
		if !spec.ops[c.Op] {
			add(p("op"), "invalid_op", fmt.Sprintf("operator %q is not valid for field %q", c.Op, c.Field))
			continue
		}
		switch spec.kind {
		case kindCities:
			var raw []string
			if err := json.Unmarshal(c.Value, &raw); err != nil || len(raw) == 0 {
				add(p("value"), "invalid_value", fmt.Sprintf("%s requires a non-empty array of city names", c.Field))
				continue
			}
			seen := map[string]bool{}
			var canon []string
			bad := false
			for _, s := range raw {
				name, ok := NormalizeCity(s)
				if !ok {
					add(p("value"), "unknown_city", fmt.Sprintf("city %q is not a known city", s))
					bad = true
					continue
				}
				if !seen[name] {
					seen[name] = true
					canon = append(canon, name)
				}
			}
			if bad {
				continue
			}
			b, _ := json.Marshal(canon)
			out.Conditions[i].Value = b
			ps[i] = parsed{ok: true, cities: canon}
		case kindLanguage:
			var s string
			if err := json.Unmarshal(c.Value, &s); err != nil || (s != "tr" && s != "en") {
				add(p("value"), "invalid_value", "language must be \"tr\" or \"en\"")
				continue
			}
			ps[i] = parsed{ok: true, str: s}
		case kindInt:
			n, ok := parseInt(c.Value)
			if !ok {
				add(p("value"), "invalid_value", fmt.Sprintf("%s requires an integer", c.Field))
				continue
			}
			if n < spec.min || n > spec.max {
				add(p("value"), "out_of_range", fmt.Sprintf("%s must be between %d and %d", c.Field, spec.min, spec.max))
				continue
			}
			ps[i] = parsed{ok: true, n: n}
		}
	}

	// Conflicts: later condition reported against earlier one.
	for j, cj := range r.Conditions {
		if !ps[j].ok {
			continue
		}
		for i := 0; i < j; i++ {
			ci := r.Conditions[i]
			if !ps[i].ok || ci.Field != cj.Field {
				continue
			}
			if conflicts(ci, ps[i], cj, ps[j]) {
				add(fmt.Sprintf("conditions[%d]", j), "conflicting_conditions",
					fmt.Sprintf("conditions[%d] conflicts with conditions[%d] on field %q", j, i, cj.Field))
				break
			}
		}
	}

	sort.SliceStable(errs, func(a, b int) bool { return condIndex(errs[a].Path) < condIndex(errs[b].Path) })
	return out, errs
}

func conflicts(a Condition, pa parsed, b Condition, pb parsed) bool {
	ac, an, bc, bn := pa.cities, pa.n, pb.cities, pb.n
	switch a.Field {
	case "city":
		if a.Op == "in" && b.Op == "in" {
			m := map[string]bool{}
			for _, c := range ac {
				m[c] = true
			}
			for _, c := range bc {
				if m[c] {
					return false
				}
			}
			return true // empty intersection
		}
		if a.Op == b.Op {
			return false
		}
		in, not := ac, bc
		if a.Op == "not_in" {
			in, not = bc, ac
		}
		ex := map[string]bool{}
		for _, c := range not {
			ex[c] = true
		}
		for _, c := range in {
			if !ex[c] {
				return false
			}
		}
		return true // every included city is also excluded
	case "language":
		return pa.str != pb.str
	case "order_count", "total_spent":
		if a.Op == b.Op {
			return false
		}
		gte, lte := an, bn
		if a.Op == "lte" {
			gte, lte = bn, an
		}
		return gte > lte
	case "purchased":
		if a.Op == b.Op {
			return false
		}
		within, notWithin := an, bn
		if a.Op == "not_within_days" {
			within, notWithin = bn, an
		}
		return notWithin >= within // purchased in last W days but not in last N>=W days
	}
	return false
}

func condIndex(path string) int {
	var i int
	if _, err := fmt.Sscanf(path, "conditions[%d]", &i); err != nil {
		return -1
	}
	return i
}

// parseInt accepts only JSON integer literals (no fraction/exponent/strings).
func parseInt(raw json.RawMessage) (int64, bool) {
	s := string(bytes.TrimSpace(raw))
	if s == "" || s[0] == '"' {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// ValidateGenerated is Validate plus the checks that apply only to model
// output: an unmapped audience must not be widened to everyone, either with
// audience "all" or with a condition every user satisfies.
func ValidateGenerated(r Rule) (Rule, verr.List) {
	out, errs := Validate(r)
	if r.Audience == "all" && len(r.Unmapped) > 0 {
		errs = append(errs, verr.Error{Path: "audience", Code: "all_with_unmapped",
			Message: "the brief names an audience that could not be mapped; leave conditions empty instead of selecting everyone"})
	}
	for i, c := range r.Conditions {
		spec, ok := whitelist[c.Field]
		if !ok || c.Op != "gte" {
			continue
		}
		if n, ok := parseInt(c.Value); ok && n <= spec.min {
			errs = append(errs, verr.Error{Path: fmt.Sprintf("conditions[%d]", i), Code: "tautological_condition",
				Message: fmt.Sprintf("%s gte %d matches every user; do not add placeholder conditions", c.Field, n)})
		}
	}
	return out, errs
}
