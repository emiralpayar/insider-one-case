package llm

import "testing"

func TestParsePartial(t *testing.T) {
	cases := []struct {
		in   string
		want PartialContent
	}{
		{``, PartialContent{}},
		{`{"sms":"Merhaba {{fir`, PartialContent{SMS: "Merhaba {{fir"}},
		{`{"sms":"a\"b","email":{"subj`, PartialContent{SMS: `a"b`}},
		{`{"sms":"x","email":{"subject":"Konu","preheader":"Ön","body":"Satır1\nSatır2 ç"}}`,
			PartialContent{SMS: "x", Subject: "Konu", Preheader: "Ön", Body: "Satır1\nSatır2 ç"}},
		{`{"email":{"subject":"S","body":"yarım \`, PartialContent{Subject: "S", Body: "yarım "}},
		{`{"email":{"subject":"S","body":"a\u00`, PartialContent{Subject: "S", Body: "a"}},
		{`{"email": {"subject" : "ok"}, "sms" : "s"}`, PartialContent{SMS: "s", Subject: "ok"}},
		{"```json\n{\"sms\":\"fenced", PartialContent{SMS: "fenced"}},
		{`{"subject":"top-level subject is not email.subject"}`, PartialContent{}},
		{`{"sms":"emoji 😀"}`, PartialContent{SMS: "emoji 😀"}},
	}
	for _, c := range cases {
		if got := ParsePartial(c.in); got != c.want {
			t.Errorf("ParsePartial(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}
