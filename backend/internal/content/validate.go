package content

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/emiralpayar/campaign-composer/internal/verr"
)

// WorstCase holds the longest recipient values used for worst-case length checks.
type WorstCase struct {
	LongestFirstName string
	LongestCity      string
}

const (
	defaultLongestFirstName = "Abdurrahman Muhammed"
	defaultLongestCity      = "Kahramanmaraş"
)

// WithDefaults fills empty fields with the default worst-case values.
func (wc WorstCase) WithDefaults() WorstCase {
	if wc.LongestFirstName == "" {
		wc.LongestFirstName = defaultLongestFirstName
	}
	if wc.LongestCity == "" {
		wc.LongestCity = defaultLongestCity
	}
	return wc
}

func has(channels []string, ch string) bool {
	for _, c := range channels {
		if c == ch {
			return true
		}
	}
	return false
}

// Validate checks c for the selected channels: required fields, placeholder
// syntax and worst-case length limits.
func Validate(c Content, channels []string, wc WorstCase) verr.List {
	wc = wc.WithDefaults()
	var errs verr.List
	if has(channels, "sms") {
		errs = append(errs, checkField("sms", c.SMS, true, SMSMaxChars, "sms_too_long",
			"SMS en kötü durumda %d karakter, sınır %d", wc)...)
	}
	if has(channels, "email") {
		errs = append(errs, checkField("email.subject", c.Email.Subject, true, SubjectMaxChars, "subject_too_long",
			"E-posta konusu en kötü durumda %d karakter, sınır %d", wc)...)
		errs = append(errs, checkField("email.body", c.Email.Body, true, 0, "", "", wc)...)
		errs = append(errs, checkField("email.preheader", c.Email.Preheader, false, 0, "", "", wc)...)
	}
	return errs
}

func checkField(path, text string, required bool, limit int, code, msgFmt string, wc WorstCase) verr.List {
	var errs verr.List
	if strings.TrimSpace(text) == "" {
		if required {
			errs = append(errs, verr.Error{Path: path, Code: "empty_field", Message: "Alan boş olamaz"})
		}
		return errs
	}
	for _, t := range scan(text) {
		switch {
		case t.kind == tokBad:
			errs = append(errs, verr.Error{Path: path, Code: "malformed_placeholder",
				Message: fmt.Sprintf("Hatalı yer tutucu: %q", t.raw)})
		case t.kind == tokVar && !allowedVars[t.text]:
			errs = append(errs, verr.Error{Path: path, Code: "unknown_variable",
				Message: fmt.Sprintf("Bilinmeyen değişken: %s (izin verilenler: first_name, city)", t.text)})
		}
	}
	if len(errs) > 0 || limit == 0 {
		return errs
	}
	longest := map[string]string{"first_name": wc.LongestFirstName, "city": wc.LongestCity}
	n := max(
		utf8.RuneCountInString(Render(text, longest)),
		utf8.RuneCountInString(Render(text, map[string]string{})),
	)
	if n > limit {
		errs = append(errs, verr.Error{Path: path, Code: code, Message: fmt.Sprintf(msgFmt, n, limit)})
	}
	return errs
}
