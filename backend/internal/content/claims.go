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
	{"sinirli", "son gün", "saat içinde", "bugüne özel"}, // folded: ı→i
}

// numberRe matches numbers with optional thousands/decimal separators
// ("30", "5.000", "1,5").
var numberRe = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

// thousandsRe is a Turkish thousands-grouped integer ("5.000", "1.250.000").
var thousandsRe = regexp.MustCompile(`^\d{1,3}(?:\.\d{3})+$`)

// normNumber makes "5.000" and "5000" compare equal while keeping "1,5" and
// "15" (or "50.00" and "5000") apart: dots are dropped only in thousands
// grouping, and a comma is the decimal separator.
func normNumber(s string) string {
	if thousandsRe.MatchString(s) {
		return strings.ReplaceAll(s, ".", "")
	}
	return strings.ReplaceAll(s, ",", ".")
}

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

// lower folds for matching: Turkish lower case, then ı→i, so "IADE" (typed
// without a Turkish keyboard), "İADE" and "iade" all match the stem "iade".
func lower(s string) string {
	return strings.ReplaceAll(strings.ToLowerSpecial(unicode.TurkishCase, s), "ı", "i")
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
