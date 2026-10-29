package content

import "fmt"

// JSONSchema returns the JSON schema (restricted keyword subset) for the
// content object of the given channels, for use as structured model output.
func JSONSchema(channels []string) map[string]any {
	props := map[string]any{}
	required := []string{}
	vars := "Yalnızca {{first_name}} ve {{city}} yer tutucuları kullanılabilir."
	if has(channels, "sms") {
		props["sms"] = map[string]any{
			"type": "string",
			"description": fmt.Sprintf("SMS metni. En fazla %d karakter (yer tutucular en uzun değerleriyle doldurulduğunda). %s",
				SMSMaxChars, vars),
		}
		required = append(required, "sms")
	}
	if has(channels, "email") {
		props["email"] = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subject": map[string]any{
					"type":        "string",
					"description": fmt.Sprintf("E-posta konusu. En fazla %d karakter. %s", SubjectMaxChars, vars),
				},
				"preheader": map[string]any{
					"type":        "string",
					"description": "E-posta ön izleme metni. " + vars,
				},
				"body": map[string]any{
					"type":        "string",
					"description": "E-posta gövdesi. " + vars,
				},
			},
			"required":             []string{"subject", "preheader", "body"},
			"additionalProperties": false,
		}
		required = append(required, "email")
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}
