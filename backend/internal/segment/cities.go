package segment

import (
	"strings"
	"unicode"

	"github.com/emiralpayar/campaign-composer/internal/geo"
)

// Cities is the canonical city list (same source as the seed).
var Cities = geo.Names()

var cityIndex = func() map[string]string {
	m := make(map[string]string, len(Cities))
	for _, c := range Cities {
		m[foldCity(c)] = c
	}
	return m
}()

// foldCity lower-cases with Turkish rules and folds dotless ı to i, so that
// İ, I, ı and i all compare equal. Used for comparison only.
func foldCity(s string) string {
	s = strings.ToLower(strings.Map(unicode.TurkishCase.ToLower, strings.TrimSpace(s)))
	return strings.ReplaceAll(s, "ı", "i")
}

// NormalizeCity returns the canonical city name for s, case-insensitively.
func NormalizeCity(s string) (string, bool) {
	c, ok := cityIndex[foldCity(s)]
	return c, ok
}
