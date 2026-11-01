package content

import (
	"fmt"
	"regexp"
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

// numberRe matches numbers with optional thousands/decimal separators
// ("30", "5.000", "1,5").
var numberRe = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

// normNumber drops separators so "5.000" and "5000" compare equal.
func normNumber(s string) string { return strings.NewReplacer(".", "", ",", "").Replace(s) }

// UnrequestedClaims reports promises in c (stock, delivery, warranty,
// returns, gifts, deadlines) that the brief does not mention, and numbers
// (discount rates, amounts, dates) that do not appear in the brief: "%50"
// when the brief says "%30". It applies to model output only (DECISIONS
// #24): a marketer editing the text by hand may add whatever they want.
func UnrequestedClaims(brief string, c Content) verr.List {
	b := lower(brief)
	briefNums := map[string]bool{}
	for _, n := range numberRe.FindAllString(brief, -1) {
		briefNums[normNumber(n)] = true
	}
	var errs verr.List
	for _, f := range []struct{ path, text string }{
		{"sms", c.SMS}, {"email.subject", c.Email.Subject},
		{"email.preheader", c.Email.Preheader}, {"email.body", c.Email.Body},
	} {
		t := lower(f.text)
		for _, n := range numberRe.FindAllString(f.text, -1) {
			if !briefNums[normNumber(n)] {
				errs = append(errs, verr.Error{Path: f.path, Code: "number_not_in_brief",
					Message: fmt.Sprintf("%q brief'te geçmiyor; oran, tutar ve tarihler brief'tekiyle aynı olmalı", n)})
			}
		}
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
