package llm

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// PartialContent holds the content string fields seen so far in a streamed,
// possibly incomplete JSON document.
type PartialContent struct {
	SMS       string `json:"sms"`
	Subject   string `json:"subject"`
	Preheader string `json:"preheader"`
	Body      string `json:"body"`
}

// ParsePartial extracts sms / email.{subject,preheader,body} from a JSON
// prefix. An unterminated string value yields what has arrived so far (an
// incomplete escape at the end is dropped). It never fails; anything it cannot
// make sense of is ignored.
func ParsePartial(text string) PartialContent {
	var out PartialContent
	i := strings.IndexByte(text, '{')
	if i < 0 {
		return out
	}
	p := &partialParser{s: text, i: i}
	p.walk(func(path []string, v string) {
		switch strings.Join(path, ".") {
		case "sms":
			out.SMS = v
		case "email.subject":
			out.Subject = v
		case "email.preheader":
			out.Preheader = v
		case "email.body":
			out.Body = v
		}
	})
	return out
}

type partialParser struct {
	s string
	i int
}

type frame struct {
	object    bool
	key       string
	expectKey bool
}

// walk scans the document and calls emit for every string value at an object
// key, with the key path of enclosing objects.
func (p *partialParser) walk(emit func(path []string, v string)) {
	var stack []frame
	path := func() []string {
		ks := make([]string, 0, len(stack))
		for _, f := range stack {
			if !f.object {
				ks = append(ks, "[]")
				continue
			}
			ks = append(ks, f.key)
		}
		return ks
	}
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '{':
			stack = append(stack, frame{object: true, expectKey: true})
			p.i++
		case c == '[':
			stack = append(stack, frame{})
			p.i++
		case c == '}' || c == ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			p.i++
		case c == ',':
			if n := len(stack); n > 0 && stack[n-1].object {
				stack[n-1].expectKey = true
			}
			p.i++
		case c == ':':
			p.i++
		case c == '"':
			v, done := p.str()
			n := len(stack)
			if n > 0 && stack[n-1].object && stack[n-1].expectKey {
				if !done {
					return
				}
				stack[n-1].key = v
				stack[n-1].expectKey = false
				continue
			}
			if n > 0 && stack[n-1].object {
				emit(path(), v)
			}
			if !done {
				return
			}
		default:
			p.i++ // whitespace, numbers, literals
		}
	}
}

// str reads a JSON string starting at the opening quote. done is false when
// the input ended before the closing quote.
func (p *partialParser) str() (string, bool) {
	var b strings.Builder
	p.i++ // opening quote
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch c {
		case '"':
			p.i++
			return b.String(), true
		case '\\':
			r, n, ok := p.escape()
			if !ok {
				p.i = len(p.s)
				return b.String(), false
			}
			b.WriteString(r)
			p.i += n
		default:
			r, n := utf8.DecodeRuneInString(p.s[p.i:])
			b.WriteRune(r)
			p.i += n
		}
	}
	return b.String(), false
}

// escape decodes the escape sequence at p.i. ok is false when it is
// incomplete (truncated input).
func (p *partialParser) escape() (string, int, bool) {
	rest := p.s[p.i:]
	if len(rest) < 2 {
		return "", 0, false
	}
	switch rest[1] {
	case '"', '\\', '/':
		return rest[1:2], 2, true
	case 'b':
		return "\b", 2, true
	case 'f':
		return "\f", 2, true
	case 'n':
		return "\n", 2, true
	case 'r':
		return "\r", 2, true
	case 't':
		return "\t", 2, true
	case 'u':
		r1, ok := hex4(rest[2:])
		if !ok {
			return "", 0, false
		}
		if !utf16.IsSurrogate(r1) {
			return string(r1), 6, true
		}
		if len(rest) < 12 {
			return "", 0, false
		}
		if rest[6] == '\\' && rest[7] == 'u' {
			if r2, ok := hex4(rest[8:]); ok {
				return string(utf16.DecodeRune(r1, r2)), 12, true
			}
		}
		return string(utf8.RuneError), 6, true
	default:
		return rest[1:2], 2, true
	}
}

func hex4(s string) (rune, bool) {
	if len(s) < 4 {
		return 0, false
	}
	v, err := strconv.ParseUint(s[:4], 16, 32)
	if err != nil {
		return 0, false
	}
	return rune(v), true
}
