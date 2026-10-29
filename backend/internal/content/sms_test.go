package content

import (
	"reflect"
	"strings"
	"testing"
)

func TestSMSInfo(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		chars    int
		enc      string
		segments int
		nonGSM   []string
	}{
		{"short gsm", "Merhaba Ali", 11, "GSM-7", 1, nil},
		{"empty", "", 0, "GSM-7", 0, nil},
		{"160 gsm", strings.Repeat("a", 160), 160, "GSM-7", 1, nil},
		{"161 gsm", strings.Repeat("a", 161), 161, "GSM-7", 2, nil},
		{"306 gsm", strings.Repeat("a", 306), 306, "GSM-7", 2, nil},
		{"307 gsm", strings.Repeat("a", 307), 307, "GSM-7", 3, nil},
		{"turkish", "Merhaba Ayşe", 12, "UCS-2", 1, []string{"ş"}},
		{"70 ucs", strings.Repeat("ş", 70), 70, "UCS-2", 1, []string{"ş"}},
		{"71 ucs", strings.Repeat("ş", 71), 71, "UCS-2", 2, []string{"ş"}},
		{"134 ucs", strings.Repeat("ş", 134), 134, "UCS-2", 2, []string{"ş"}},
		{"135 ucs", strings.Repeat("ş", 135), 135, "UCS-2", 3, []string{"ş"}},
		{"euro", "€", 2, "GSM-7", 1, nil},
		{"brace", "{", 2, "GSM-7", 1, nil},
		{"ext pushes over", strings.Repeat("a", 159) + "€", 161, "GSM-7", 2, nil},
		{"ucs ext counts one", "ş{", 2, "UCS-2", 1, []string{"ş"}},
		{"dedupe order", "çşç ğ", 5, "UCS-2", 1, []string{"ç", "ş", "ğ"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SMSInfo(c.in)
			if got.Chars != c.chars || got.Encoding != c.enc || got.Segments != c.segments {
				t.Fatalf("got %+v", got)
			}
			if len(got.NonGSMChars) != len(c.nonGSM) || (len(c.nonGSM) > 0 && !reflect.DeepEqual(got.NonGSMChars, c.nonGSM)) {
				t.Fatalf("nonGSM got %v want %v", got.NonGSMChars, c.nonGSM)
			}
		})
	}
}

func TestSMSInfoFullBaseTable(t *testing.T) {
	base := "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"
	got := SMSInfo(base)
	if got.Encoding != "GSM-7" {
		t.Fatalf("base table must be GSM-7, nonGSM=%v", got.NonGSMChars)
	}
	got = SMSInfo("^{}\\[~]|€\f")
	if got.Encoding != "GSM-7" || got.Chars != 20 {
		t.Fatalf("ext table: %+v", got)
	}
}
