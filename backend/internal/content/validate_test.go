package content

import (
	"strings"
	"testing"

	"github.com/emiralpayar/campaign-composer/internal/verr"
)

var wc = WorstCase{LongestFirstName: "Abdurrahman Muhammed", LongestCity: "Kahramanmaraş"}

func find(l verr.List, path, code string) *verr.Error {
	for i := range l {
		if l[i].Path == path && l[i].Code == code {
			return &l[i]
		}
	}
	return nil
}

func TestValidateSMSWorstCase(t *testing.T) {
	c := Content{SMS: strings.Repeat("a", 148) + "{{first_name}}"}
	errs := Validate(c, []string{"sms"}, wc)
	e := find(errs, "sms", "sms_too_long")
	if e == nil {
		t.Fatalf("want sms_too_long, got %v", errs)
	}
	if e.Message != "SMS en kötü durumda 168 karakter, sınır 160" {
		t.Fatalf("message %q", e.Message)
	}
}

func TestValidateSMSOK(t *testing.T) {
	c := Content{SMS: strings.Repeat("a", 140) + "{{first_name}}"}
	if errs := Validate(c, []string{"sms"}, wc); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestValidateDefaultWorstCase(t *testing.T) {
	c := Content{SMS: strings.Repeat("a", 148) + "{{first_name}}"}
	if find(Validate(c, []string{"sms"}, WorstCase{}), "sms", "sms_too_long") == nil {
		t.Fatal("default worst case should apply")
	}
}

func TestValidateSubject(t *testing.T) {
	c := Content{Email: Email{Subject: strings.Repeat("a", 81), Body: "x"}}
	errs := Validate(c, []string{"email"}, wc)
	e := find(errs, "email.subject", "subject_too_long")
	if e == nil || e.Message != "E-posta konusu en kötü durumda 81 karakter, sınır 80" {
		t.Fatalf("%v", errs)
	}
	c.Email.Subject = strings.Repeat("ş", 80)
	if errs := Validate(c, []string{"email"}, wc); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestValidateBrokenPlaceholders(t *testing.T) {
	cases := []struct{ sms, code string }{
		{"{{phone}}", "unknown_variable"},
		{"Merhaba {{first_name", "malformed_placeholder"},
		{"}}", "malformed_placeholder"},
		{"{{}}", "malformed_placeholder"},
		{"{{First}}", "malformed_placeholder"},
		{"{{{first_name}}}", "malformed_placeholder"},
		{"{{first_name}} }}", "malformed_placeholder"},
	}
	for _, c := range cases {
		errs := Validate(Content{SMS: c.sms}, []string{"sms"}, wc)
		if find(errs, "sms", c.code) == nil {
			t.Errorf("%q: want %s got %v", c.sms, c.code, errs)
		}
	}
	errs := Validate(Content{SMS: "{{phone}}"}, []string{"sms"}, wc)
	if !strings.Contains(errs[0].Message, "phone") {
		t.Fatal(errs[0].Message)
	}
}

func TestValidateLengthCheckedDespitePlaceholderErrors(t *testing.T) {
	// Both problems are reported at once so one LLM repair round can fix both.
	errs := Validate(Content{SMS: "{{phone}} " + strings.Repeat("a", 400)}, []string{"sms"}, wc)
	if find(errs, "sms", "unknown_variable") == nil || find(errs, "sms", "sms_too_long") == nil {
		t.Fatal(errs)
	}
}

func TestValidateSingleBracesOK(t *testing.T) {
	if errs := Validate(Content{SMS: "a { b } c {x}"}, []string{"sms"}, wc); len(errs) != 0 {
		t.Fatal(errs)
	}
	if errs := Validate(Content{SMS: "{{ first_name }} {{city}}"}, []string{"sms"}, wc); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestValidateChannels(t *testing.T) {
	if errs := Validate(Content{SMS: "hi"}, []string{"sms"}, wc); len(errs) != 0 {
		t.Fatal(errs)
	}
	errs := Validate(Content{SMS: "hi"}, []string{"sms", "email"}, wc)
	if find(errs, "email.subject", "empty_field") == nil || find(errs, "email.body", "empty_field") == nil {
		t.Fatal(errs)
	}
	if find(errs, "email.preheader", "empty_field") != nil {
		t.Fatal("preheader optional")
	}
	errs = Validate(Content{}, []string{"sms"}, wc)
	if find(errs, "sms", "empty_field") == nil {
		t.Fatal(errs)
	}
}

func TestValidateEmailPlaceholderPaths(t *testing.T) {
	c := Content{Email: Email{Subject: "s", Preheader: "{{x}}", Body: "{{first_name"}}
	errs := Validate(c, []string{"email"}, wc)
	if find(errs, "email.preheader", "unknown_variable") == nil || find(errs, "email.body", "malformed_placeholder") == nil {
		t.Fatal(errs)
	}
}

func TestJSONSchema(t *testing.T) {
	s := JSONSchema([]string{"sms", "email"})
	props := s["properties"].(map[string]any)
	if _, ok := props["sms"]; !ok {
		t.Fatal("sms")
	}
	if s["additionalProperties"] != false {
		t.Fatal("additionalProperties")
	}
	if req := s["required"].([]string); len(req) != 2 {
		t.Fatal(req)
	}
	s = JSONSchema([]string{"sms"})
	if _, ok := s["properties"].(map[string]any)["email"]; ok {
		t.Fatal("email should be absent")
	}
}

// A Turkish suffix glued to a placeholder ("{{city}}'daki") follows vowel
// harmony of a value we do not know, so it is wrong for some recipients.
func TestValidatePlaceholderSuffix(t *testing.T) {
	bad := []string{"{{city}}'daki fırsat", "{{city}}’de", "{{first_name}}cım", "Selam {{first_name}}'e"}
	for _, s := range bad {
		if find(Validate(Content{SMS: s}, []string{"sms"}, wc), "sms", "placeholder_suffix") == nil {
			t.Errorf("%q: want placeholder_suffix", s)
		}
	}
	ok := []string{"Merhaba {{first_name}}, {{city}} için", "{{city}}: fırsat", "Selam {{first_name}}!", "{{first_name}}"}
	for _, s := range ok {
		if errs := Validate(Content{SMS: s}, []string{"sms"}, wc); len(errs) != 0 {
			t.Errorf("%q: %v", s, errs)
		}
	}
}

func TestValidateSingleBracePlaceholder(t *testing.T) {
	c := Content{SMS: "Merhaba {first_name}, { city } için indirim", Email: Email{Subject: "x", Body: "y {}"}}
	errs := Validate(c, []string{"sms", "email"}, WorstCase{}.WithDefaults())
	n := 0
	for _, e := range errs {
		if e.Code == "malformed_placeholder" && e.Path == "sms" {
			n++
		}
		if e.Path == "email.body" {
			t.Fatalf("empty braces flagged: %+v", e)
		}
	}
	if n != 1 {
		t.Fatalf("errs = %v", errs)
	}
}
