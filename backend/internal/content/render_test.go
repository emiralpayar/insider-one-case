package content

import "testing"

func TestRender(t *testing.T) {
	ayse := map[string]string{"first_name": "Ayşe", "city": "İzmir"}
	empty := map[string]string{"first_name": "", "city": ""}
	cases := []struct {
		name, tmpl string
		vars       map[string]string
		want       string
	}{
		{"basic", "Merhaba {{first_name}}, sepetin seni bekliyor!", ayse, "Merhaba Ayşe, sepetin seni bekliyor!"},
		{"empty name", "Merhaba {{first_name}}, sepetin seni bekliyor!", empty, "Merhaba, sepetin seni bekliyor!"},
		{"leading", "{{first_name}}, sepetin seni bekliyor!", empty, "Sepetin seni bekliyor!"},
		{"turkish upper", "{{first_name}}, indirim seni bekliyor!", empty, "İndirim seni bekliyor!"},
		{"leading bang", "{{first_name}}! ışık", empty, "Işık"},
		{"space before bang", "Selam {{first_name}} !", empty, "Selam!"},
		{"double space", "Selam  {{first_name}}  dünya", empty, "Selam dünya"},
		{"spaced braces", "Selam {{ first_name }}!", ayse, "Selam Ayşe!"},
		{"city", "{{city}}'de fırsat", ayse, "İzmir'de fırsat"},
		{"no cleanup when not empty", "A  B , C", ayse, "A  B , C"},
		{"no cleanup when no placeholder", "A  b !", empty, "A  b !"},
		{"double comma", "Merhaba {{first_name}}, {{city}}, hoş geldin", empty, "Merhaba, hoş geldin"},
		{"unknown left", "Hi {{phone}}", ayse, "Hi {{phone}}"},
		{"malformed left", "Hi {{first_name", ayse, "Hi {{first_name"},
		{"stray close", "Hi }} {{first_name}}", ayse, "Hi }} Ayşe"},
		{"multiline", "{{first_name}}, merhaba\n\nİyi günler {{first_name}} !", empty, "Merhaba\n\nİyi günler!"},
		{"missing var is empty", "Selam {{first_name}} !", nil, "Selam!"},
		{"multiline keeps untouched lines", "a  b\nSelam {{first_name}} !", empty, "a  b\nSelam!"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Render(c.tmpl, c.vars); got != c.want {
				t.Fatalf("Render(%q) = %q, want %q", c.tmpl, got, c.want)
			}
		})
	}
}
