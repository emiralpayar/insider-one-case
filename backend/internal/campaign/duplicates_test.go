package campaign_test

import (
	"context"
	"errors"
	"testing"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
)

// A second campaign with the same text and audience needs explicit
// confirmation; changing the text removes the warning.
func TestApproveDuplicateNeedsConfirm(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	store := campaign.NewStore(pool, "mock")

	first := readyCampaign(t, pool, istanbulAbandoned())
	if _, err := campaign.Approve(ctx, pool, first, campaign.ApproveRequest{}); err != nil {
		t.Fatal(err)
	}

	second := readyCampaign(t, pool, istanbulAbandoned())
	c, err := store.Get(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	dups, err := campaign.DuplicateSends(ctx, pool, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 2 || dups[0].CampaignID != first || dups[0].Recipients == 0 || !dups[0].SameText {
		t.Fatalf("duplicates = %+v", dups)
	}
	if _, err := campaign.Approve(ctx, pool, second, campaign.ApproveRequest{}); !errors.Is(err, campaign.ErrDuplicateNotConfirmed) {
		t.Fatalf("approve without confirm: %v", err)
	}
	if _, err := campaign.Approve(ctx, pool, second, campaign.ApproveRequest{ConfirmDuplicate: true}); err != nil {
		t.Fatalf("approve with confirm: %v", err)
	}

	// Same brief, different text (what Claude usually produces): still reported.
	third := readyCampaign(t, pool, istanbulAbandoned())
	if _, err := pool.Exec(ctx, `update campaigns set channels = '{sms}',
		content = jsonb_set(content, '{sms}', '"Farklı bir metin {{first_name}}"') where id=$1`, third); err != nil {
		t.Fatal(err)
	}
	c, _ = store.Get(ctx, third)
	dups, _ = campaign.DuplicateSends(ctx, pool, c)
	if len(dups) == 0 || dups[0].Channel != "sms" || dups[0].SameText {
		t.Fatalf("same brief, different text: %+v", dups)
	}
	// Different brief and different text: nothing.
	if _, err := pool.Exec(ctx, `update campaigns set brief = '  ANKARA için   başka bir kampanya' where id=$1`, third); err != nil {
		t.Fatal(err)
	}
	c, _ = store.Get(ctx, third)
	if dups, _ := campaign.DuplicateSends(ctx, pool, c); len(dups) != 0 {
		t.Fatalf("unrelated campaign reported as duplicate: %+v", dups)
	}
}
