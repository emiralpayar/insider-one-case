package campaign_test

import (
	"context"
	"testing"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
)

// Regenerating SMS must not write back a stale email: the email saved by
// hand while the SMS stream ran survives.
func TestSaveContentChannelsKeepsOtherChannel(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	st := campaign.NewStore(pool, "mock")
	id := readyCampaign(t, pool, istanbulAbandoned())

	stale := goodContent() // read when the SMS stream started
	edited := goodContent()
	edited.Email.Subject = "Elle düzeltilmiş konu"
	if err := st.SaveContent(ctx, id, edited); err != nil { // saved meanwhile
		t.Fatal(err)
	}
	regen := stale
	regen.SMS = "Merhaba {{first_name}}, yeni SMS."
	got, err := st.SaveContentChannels(ctx, id, regen, []string{"sms"})
	if err != nil {
		t.Fatal(err)
	}
	if got.SMS != regen.SMS || got.Email.Subject != "Elle düzeltilmiş konu" {
		t.Fatalf("stored = %+v", got)
	}
	c, _ := st.Get(ctx, id)
	if c.Content.Email.Subject != "Elle düzeltilmiş konu" || c.Content.SMS != regen.SMS {
		t.Fatalf("db = %+v", c.Content)
	}
}
