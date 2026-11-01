package content

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
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
		errs = append(errs, checkField("email.body", c.Email.Body, true, EmailBodyMaxChars, "body_too_long",
			"E-posta gövdesi en kötü durumda %d karakter, sınır %d", wc)...)
		errs = append(errs, checkField("email.preheader", c.Email.Preheader, false, PreheaderMaxChars, "preheader_too_long",
			"Ön başlık en kötü durumda %d karakter, sınır %d", wc)...)
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
	toks := scan(text)
	for i, t := range toks {
		switch {
		case t.kind == tokLit && singleBraceRe.MatchString(t.text):
			errs = append(errs, verr.Error{Path: path, Code: "malformed_placeholder",
				Message: fmt.Sprintf("Tek süslü parantezli yer tutucu: %q (doğrusu {{%s}})", singleBraceRe.FindString(t.text), singleBraceRe.FindStringSubmatch(t.text)[1])})
		case t.kind == tokBad:
			errs = append(errs, verr.Error{Path: path, Code: "malformed_placeholder",
				Message: fmt.Sprintf("Hatalı yer tutucu: %q", t.raw)})
		case t.kind == tokVar && !allowedVars[t.text]:
			errs = append(errs, verr.Error{Path: path, Code: "unknown_variable",
				Message: fmt.Sprintf("Bilinmeyen değişken: %s (izin verilenler: first_name, city)", t.text)})
		case t.kind == tokVar && i+1 < len(toks) && toks[i+1].kind == tokLit && gluedSuffix(toks[i+1].text):
			errs = append(errs, verr.Error{Path: path, Code: "placeholder_suffix",
				Message: fmt.Sprintf("{{%s}} değişkenine ek getirilmiş; değer bilinmediği için ek bazı alıcılarda yanlış olur (ör. \"{{city}} için\" yazın)", t.text)})
		}
	}
	if limit == 0 {
		return errs
	}
	// Checked even when placeholders are broken, so a repair round sees every
	// problem at once. Unknown/malformed tokens render raw: an over-estimate.
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

// singleBraceRe finds an allowed variable written with single braces
// ("{first_name}", "{ city }") in literal text: it would reach recipients
// verbatim, since only {{name}} is substituted. Other single-brace text
// ("{x}") stays allowed.
var singleBraceRe = regexp.MustCompile(`\{\s*(first_name|city)\s*\}`)

// gluedSuffix reports whether lit, the text right after a placeholder, starts
// with a suffix attached to it: an apostrophe or a letter.
func gluedSuffix(lit string) bool {
	r, _ := utf8.DecodeRuneInString(lit)
	return r == '\'' || r == '’' || unicode.IsLetter(r)
}
