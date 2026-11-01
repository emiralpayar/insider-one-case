package content

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/emiralpayar/campaign-composer/internal/verr"
)

// claimGroups are promises generated content must not make unless the brief
// does. Any word of a group in the brief allows the whole group ("kargo
// bedava" allows "ücretsiz kargo"). Matching is by lowercase substring, so
// stems cover inflections ("stok" matches "stoklar", "tüken" "tükenmeden").
var claimGroups = [][]string{
	{"stok", "tüken"},
	{"kargo", "teslimat", "ücretsiz", "bedava"},
	{"garanti"},
	{"iade"},
	{"hediye"},
	{"sınırlı", "son gün", "saat içinde", "bugüne özel"},
}

// UnrequestedClaims reports promises in c (stock, delivery, warranty,
// returns, gifts, deadlines) that the brief does not mention. It applies to
// model output only (DECISIONS #24): a marketer editing the text by hand may
// add whatever they want.
func UnrequestedClaims(brief string, c Content) verr.List {
	b := lower(brief)
	var errs verr.List
	for _, f := range []struct{ path, text string }{
		{"sms", c.SMS}, {"email.subject", c.Email.Subject},
		{"email.preheader", c.Email.Preheader}, {"email.body", c.Email.Body},
	} {
		t := lower(f.text)
		for _, g := range claimGroups {
			if containsAny(b, g) {
				continue
			}
			for _, w := range g {
				if strings.Contains(t, w) {
					errs = append(errs, verr.Error{Path: f.path, Code: "unrequested_claim",
						Message: fmt.Sprintf("%q brief'te geçmeyen bir vaat; brief'te olmayan bilgi ekleme", w)})
				}
			}
		}
	}
	return errs
}

func lower(s string) string { return strings.ToLowerSpecial(unicode.TurkishCase, s) }

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
