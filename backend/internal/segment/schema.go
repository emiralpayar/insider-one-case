package segment

import "sort"

// JSONSchema returns the structured-output schema for Claude. It uses only the
// supported subset (type, properties, required, additionalProperties, enum,
// items, anyOf, description); ranges live in descriptions and are enforced by
// Validate.
func JSONSchema() map[string]any {
	fields := make([]any, 0, len(whitelist))
	opSet := map[string]bool{}
	names := make([]string, 0, len(whitelist))
	for f := range whitelist {
		names = append(names, f)
	}
	sort.Strings(names)
	for _, f := range names {
		fields = append(fields, f)
		for op := range whitelist[f].ops {
			opSet[op] = true
		}
	}
	ops := make([]string, 0, len(opSet))
	for op := range opSet {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	opEnum := make([]any, len(ops))
	for i, o := range ops {
		opEnum[i] = o
	}
	str := map[string]any{"type": "string"}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"audience", "conditions", "unmapped", "explanation"},
		"properties": map[string]any{
			"audience": map[string]any{
				"type": "string", "enum": []any{"all", "filtered"},
				"description": "\"all\" only if the user explicitly asks for every user (then conditions must be empty); otherwise \"filtered\" with at least one condition.",
			},
			"conditions": map[string]any{
				"type":        "array",
				"description": "Combined with AND. Allowed field/op/value: city in|not_in [known city names: " + joinCities() + "]; language eq \"tr\"|\"en\"; abandoned_cart within_days 1-365 (cart add in last N days and no purchase after the latest cart add); purchased within_days|not_within_days 1-365; order_count gte|lte 0-1000; total_spent gte|lte 0-1000000; signed_up within_days 1-3650. Numeric values are integers.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []any{"field", "op", "value"},
					"properties": map[string]any{
						"field": map[string]any{"type": "string", "enum": fields},
						"op":    map[string]any{"type": "string", "enum": opEnum},
						"value": map[string]any{
							"description": "Integer for numeric fields, \"tr\"/\"en\" string for language, array of city name strings for city.",
							"anyOf": []any{
								map[string]any{"type": "integer"},
								str,
								map[string]any{"type": "array", "items": str},
							},
						},
					},
				},
			},
			"unmapped": map[string]any{
				"type": "array", "items": str,
				"description": "Parts of the brief that have no matching field (e.g. \"VIP customers\"). Never silently dropped.",
			},
			"explanation": map[string]any{
				"type": "string", "description": "Human-readable explanation of how the brief was understood.",
			},
		},
	}
}

func joinCities() string {
	s := ""
	for i, c := range Cities {
		if i > 0 {
			s += ", "
		}
		s += c
	}
	return s
}
