package content

import "unicode/utf8"

// SMSStats describes how an SMS body is encoded and segmented.
type SMSStats struct {
	Chars       int      `json:"chars"`
	Encoding    string   `json:"encoding"` // "GSM-7" or "UCS-2"
	Segments    int      `json:"segments"`
	NonGSMChars []string `json:"non_gsm_chars"`
}

const (
	gsmBase = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"
	gsmExt  = "^{}\\[~]|€\f"
)

var gsmBaseSet, gsmExtSet = runeSet(gsmBase), runeSet(gsmExt)

func runeSet(s string) map[rune]bool {
	m := map[rune]bool{}
	for _, r := range s {
		m[r] = true
	}
	return m
}

// SMSInfo computes encoding, length and segment count of s.
// GSM-7 counts extended characters as 2; UCS-2 counts runes.
// An empty string has 0 segments.
func SMSInfo(s string) SMSStats {
	st := SMSStats{Encoding: "GSM-7", NonGSMChars: []string{}}
	gsmLen := 0
	seen := map[rune]bool{}
	for _, r := range s {
		switch {
		case gsmBaseSet[r]:
			gsmLen++
		case gsmExtSet[r]:
			gsmLen += 2
		default:
			if !seen[r] {
				seen[r] = true
				st.NonGSMChars = append(st.NonGSMChars, string(r))
			}
		}
	}
	single, multi := 160, 153
	if len(st.NonGSMChars) > 0 {
		st.Encoding = "UCS-2"
		st.Chars = utf8.RuneCountInString(s)
		single, multi = 70, 67
	} else {
		st.Chars = gsmLen
	}
	switch {
	case st.Chars == 0:
		st.Segments = 0
	case st.Chars <= single:
		st.Segments = 1
	default:
		st.Segments = (st.Chars + multi - 1) / multi
	}
	return st
}
