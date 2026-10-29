// Package content renders, validates and measures campaign message templates.
package content

const (
	// SMSMaxChars is the maximum SMS length (runes) in the worst case.
	SMSMaxChars = 160
	// SubjectMaxChars is the maximum email subject length (runes) in the worst case.
	SubjectMaxChars = 80
)

// Content is the generated message content for all channels.
type Content struct {
	SMS   string `json:"sms"`
	Email Email  `json:"email"`
}

// Email is the email part of Content.
type Email struct {
	Subject   string `json:"subject"`
	Preheader string `json:"preheader"`
	Body      string `json:"body"`
}

// allowedVars are the placeholder names templates may use.
var allowedVars = map[string]bool{"first_name": true, "city": true}
