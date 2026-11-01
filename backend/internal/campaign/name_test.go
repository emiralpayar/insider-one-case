package campaign_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/dbtest"
	"github.com/emiralpayar/campaign-composer/internal/verr"
	"github.com/google/uuid"
)

func TestDefaultName(t *testing.T) {
	long := "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder."
	for _, tc := range []struct{ brief, want string }{
		{"Kısa brief", "Kısa brief"},
		{"  çok   boşluklu\n\nbrief  ", "çok boşluklu brief"},
		{strings.Repeat("a", 60), strings.Repeat("a", 60)},
		{long, "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan…"},
		// the 61st character is a space: the 60-character prefix is whole words
		{strings.Repeat("abcd ", 12) + "xyz", strings.Repeat("abcd ", 11) + "abcd…"},
		// one long word: cut at 60 characters
		{strings.Repeat("ş", 70), strings.Repeat("ş", 60) + "…"},
		// trailing punctuation before the cut is dropped
		{strings.Repeat("x", 55) + ", " + strings.Repeat("y", 10), strings.Repeat("x", 55) + "…"},
	} {
		got := campaign.DefaultName(tc.brief)
		if got != tc.want {
			t.Errorf("DefaultName(%q) = %q, want %q", tc.brief, got, tc.want)
		}
		if n := utf8.RuneCountInString(got); n > 61 {
			t.Errorf("DefaultName(%q) has %d runes", tc.brief, n)
		}
	}
}

func TestValidateName(t *testing.T) {
	if n, errs := campaign.ValidateName("  Bahar   kampanyası "); errs != nil || n != "Bahar kampanyası" {
		t.Fatalf("got %q %v", n, errs)
	}
	if _, errs := campaign.ValidateName(" \n "); len(errs) != 1 || errs[0].Code != "empty_field" {
		t.Fatalf("empty: %v", errs)
	}
	if _, errs := campaign.ValidateName(strings.Repeat("ğ", 120)); errs != nil {
		t.Fatalf("120 runes: %v", errs)
	}
	if _, errs := campaign.ValidateName(strings.Repeat("ğ", 121)); len(errs) != 1 || errs[0].Code != "too_long" {
		t.Fatalf("121 runes: %v", errs)
	}
}

func TestCreateNamesAndRename(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	st := campaign.NewStore(pool, "mock")
	brief := "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara indirim"
	c, err := st.Create(ctx, brief)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != campaign.DefaultName(brief) {
		t.Fatalf("name = %q", c.Name)
	}
	r, err := st.Rename(ctx, c.ID, "  Sepet hatırlatma — İstanbul ")
	if err != nil || r.Name != "Sepet hatırlatma — İstanbul" {
		t.Fatalf("rename: %+v %v", r, err)
	}
	if got, _ := st.Get(ctx, c.ID); got.Name != r.Name || got.Brief != brief {
		t.Fatalf("after rename: %+v", got)
	}
	var vl verr.List
	if _, err := st.Rename(ctx, c.ID, ""); !errors.As(err, &vl) {
		t.Fatalf("empty rename: %v", err)
	}
	if _, err := st.Rename(ctx, uuid.New(), "x"); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestRenameAllowedAfterApproval(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	id := readyCampaign(t, pool, istanbulAbandoned())
	if _, err := campaign.Approve(ctx, pool, id, campaign.ApproveRequest{}); err != nil {
		t.Fatal(err)
	}
	c, err := campaign.NewStore(pool, "mock").Rename(ctx, id, "Gönderilmiş kampanya")
	if err != nil || c.Name != "Gönderilmiş kampanya" || c.Status != campaign.StatusSending {
		t.Fatalf("rename after approve: %+v %v", c, err)
	}
}
