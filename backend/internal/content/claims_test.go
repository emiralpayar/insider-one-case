package content

import "testing"

func TestUnrequestedClaims(t *testing.T) {
	cases := []struct {
		name, brief string
		c           Content
		want        []string // paths with an unrequested_claim error
	}{
		{"clean", "Sepet hatırlatması, %30 indirim.",
			Content{SMS: "Merhaba {{first_name}}, sepetindeki ürünlerde %30 indirim seni bekliyor.", Email: Email{Subject: "Sepetin seni bekliyor", Body: "Hemen incele."}}, nil},
		{"invented stock and delivery", "Sepet hatırlatması.",
			Content{SMS: "Stoklar tükenmeden al!", Email: Email{Subject: "Hızlı teslimat", Body: "Sınırlı sayıda."}},
			[]string{"sms", "sms", "email.subject", "email.body"}},
		{"group allowed by brief", "Ankara'ya kargo bedava mesajı.",
			Content{SMS: "Ücretsiz kargo fırsatı!", Email: Email{Subject: "Kargo bedava", Body: "Teslimat ücretsiz."}}, nil},
		{"turkish case", "Yılbaşı indirimi.",
			Content{SMS: "SON GÜN! HEDİYE seni bekliyor.", Email: Email{Subject: "x", Body: "y"}},
			[]string{"sms", "sms"}},
		{"number differs", "Sepet hatırlatması, %30 indirim.",
			Content{SMS: "Sepetinde %50 indirim!", Email: Email{Subject: "%30 indirim", Body: "5.000 TL üzeri"}},
			[]string{"sms", "email.body"}},
		{"numbers match with separators", "5000 TL üzeri harcamaya %10 indirim.",
			Content{SMS: "5.000 TL üzeri %10 indirim", Email: Email{Subject: "%10", Body: "5000 TL"}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := UnrequestedClaims(tc.brief, tc.c)
			if len(errs) != len(tc.want) {
				t.Fatalf("got %v, want paths %v", errs, tc.want)
			}
			for i, e := range errs {
				if (e.Code != "unrequested_claim" && e.Code != "number_not_in_brief") || e.Path != tc.want[i] {
					t.Fatalf("error %d = %+v, want path %s", i, e, tc.want[i])
				}
			}
		})
	}
}
