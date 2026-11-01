package segment

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Compile turns a rule into a WHERE fragment against `users u`. Placeholders
// start at $argOffset+1. Every value is a bind parameter; SQL text comes only
// from the fixed snippets below, selected by whitelisted field/op keys.
func Compile(r Rule, argOffset int, asOf time.Time) (string, []any, error) {
	r, errs := Validate(r)
	if len(errs) > 0 {
		return "", nil, errs
	}
	if r.Audience == "all" {
		return "TRUE", nil, nil
	}
	if len(r.Conditions) == 0 { // incomplete draft, see Validate
		return "FALSE", nil, nil
	}
	var args []any
	// add appends an arg and returns its placeholder.
	add := func(v any, cast string) string {
		args = append(args, v)
		return fmt.Sprintf("$%d%s", argOffset+len(args), cast)
	}
	parts := make([]string, 0, len(r.Conditions))
	for _, c := range r.Conditions {
		key := c.Field + "." + c.Op
		tmpl, ok := sqlTemplates[key]
		if !ok { // unreachable after Validate; defensive
			return "", nil, fmt.Errorf("segment: no SQL for %q", key)
		}
		var sub []any
		switch whitelist[c.Field].kind {
		case kindCities:
			var cs []string
			if err := json.Unmarshal(c.Value, &cs); err != nil {
				return "", nil, err
			}
			sub = []any{add(cs, "::text[]")}
		case kindLanguage:
			var s string
			if err := json.Unmarshal(c.Value, &s); err != nil {
				return "", nil, err
			}
			sub = []any{add(s, "::text")}
		case kindInt:
			n, _ := parseInt(c.Value)
			switch c.Field {
			case "total_spent":
				sub = []any{add(n, "::numeric")}
			case "order_count":
				sub = []any{add(n, "::bigint")}
			default: // *_days: asOf first, then days
				sub = []any{add(asOf, "::timestamptz"), add(int32(n), "::int")}
			}
		}
		parts = append(parts, "("+fmt.Sprintf(tmpl, sub...)+")")
	}
	return strings.Join(parts, " AND "), args, nil
}

const purchasesSince = `SELECT 1 FROM events p WHERE p.user_id=u.id AND p.type='purchase' AND p.occurred_at >= %[1]s - make_interval(days => %[2]s) AND p.occurred_at <= %[1]s`

// sqlTemplates maps whitelisted field.op keys to fixed SQL; %s/%[n]s slots
// receive only generated placeholders ("$3::int"), never user data.
var sqlTemplates = map[string]string{
	"city.in":     `u.city = ANY(%s)`,
	"city.not_in": `NOT (u.city = ANY(%s))`,
	"language.eq": `u.language = %s`,
	"abandoned_cart.within_days": `EXISTS (SELECT 1 FROM events c WHERE c.user_id=u.id AND c.type='cart_add' ` +
		`AND c.occurred_at >= %[1]s - make_interval(days => %[2]s) AND c.occurred_at <= %[1]s ` +
		`AND NOT EXISTS (SELECT 1 FROM events p WHERE p.user_id=u.id AND p.type='purchase' ` +
		`AND p.occurred_at <= %[1]s ` +
		`AND p.occurred_at > (SELECT max(occurred_at) FROM events c2 WHERE c2.user_id=u.id AND c2.type='cart_add' AND c2.occurred_at <= %[1]s)))`,
	"purchased.within_days":     `EXISTS (` + purchasesSince + `)`,
	"purchased.not_within_days": `NOT EXISTS (` + purchasesSince + `)`,
	"order_count.gte":           `(SELECT count(*) FROM events o WHERE o.user_id=u.id AND o.type='purchase') >= %s`,
	"order_count.lte":           `(SELECT count(*) FROM events o WHERE o.user_id=u.id AND o.type='purchase') <= %s`,
	"total_spent.gte":           `COALESCE((SELECT sum(o.amount) FROM events o WHERE o.user_id=u.id AND o.type='purchase'),0) >= %s`,
	"total_spent.lte":           `COALESCE((SELECT sum(o.amount) FROM events o WHERE o.user_id=u.id AND o.type='purchase'),0) <= %s`,
	"signed_up.within_days":     `u.created_at >= %[1]s - make_interval(days => %[2]s) AND u.created_at <= %[1]s`,
}
