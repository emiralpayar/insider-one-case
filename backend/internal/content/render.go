package content

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokKind int

const (
	tokLit tokKind = iota
	tokVar
	tokBad
)

type token struct {
	kind tokKind
	text string // literal text, variable name, or raw malformed text
	raw  string // original source text of the token
}

var nameRe = regexp.MustCompile(`^[a-z_]+$`)

// scan splits s into literals, {{ name }} placeholders and malformed pieces.
func scan(s string) []token {
	var out []token
	for len(s) > 0 {
		o := strings.Index(s, "{{")
		c := strings.Index(s, "}}")
		if o < 0 && c < 0 {
			out = append(out, token{tokLit, s, s})
			break
		}
		if c >= 0 && (o < 0 || c < o) { // stray "}}"
			if c > 0 {
				out = append(out, token{tokLit, s[:c], s[:c]})
			}
			out = append(out, token{tokBad, "}}", "}}"})
			s = s[c+2:]
			continue
		}
		if o > 0 {
			out = append(out, token{tokLit, s[:o], s[:o]})
		}
		end := strings.Index(s[o+2:], "}}")
		if end < 0 { // unterminated
			out = append(out, token{tokBad, s[o:], s[o:]})
			break
		}
		end += o + 2
		raw := s[o : end+2]
		name := strings.Trim(s[o+2:end], " ")
		if nameRe.MatchString(name) {
			out = append(out, token{tokVar, name, raw})
		} else {
			out = append(out, token{tokBad, raw, raw})
		}
		s = s[end+2:]
	}
	return out
}

// Render substitutes {{first_name}} / {{city}} placeholders from vars.
// Unknown or malformed placeholders are left untouched. When a substituted
// value is empty, the affected lines are tidied (see cleanLine).
func Render(tmpl string, vars map[string]string) string {
	lines := []string{""}
	dirty := []bool{false}
	write := func(s string) {
		parts := strings.Split(s, "\n")
		for i, p := range parts {
			if i > 0 {
				lines = append(lines, "")
				dirty = append(dirty, false)
			}
			lines[len(lines)-1] += p
		}
	}
	for _, t := range scan(tmpl) {
		switch {
		case t.kind == tokVar && allowedVars[t.text]:
			v := vars[t.text]
			if v == "" {
				dirty[len(dirty)-1] = true
			}
			write(v)
		default:
			write(t.raw)
		}
	}
	for i := range lines {
		if dirty[i] {
			lines[i] = cleanLine(lines[i])
		}
	}
	return strings.Join(lines, "\n")
}

var (
	multiSpaceRe  = regexp.MustCompile(` {2,}`)
	spaceBeforeRe = regexp.MustCompile(` +([,.!?;:])`)
	dupCommaRe    = regexp.MustCompile(`,(?: *,)+`)
	leadPunctRe   = regexp.MustCompile(`^[,.!?;:] +`)
)

// cleanLine tidies a line after an empty value was substituted.
func cleanLine(l string) string {
	l = multiSpaceRe.ReplaceAllString(l, " ")
	l = spaceBeforeRe.ReplaceAllString(l, "$1")
	l = dupCommaRe.ReplaceAllString(l, ",")
	l = strings.Trim(l, " \t")
	if loc := leadPunctRe.FindStringIndex(l); loc != nil {
		l = l[loc[1]:]
		r, size := utf8.DecodeRuneInString(l)
		if size > 0 {
			l = string(unicode.TurkishCase.ToUpper(r)) + l[size:]
		}
	}
	return l
}
