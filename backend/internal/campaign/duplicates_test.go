package campaign_test

import (
	"context"
	"errors"
	"testing"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/google/uuid"
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
	rep, err := campaign.DuplicateSends(ctx, pool, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	dups := rep.Duplicates
	if len(dups) != 2 || dups[0].CampaignID != first || dups[0].Recipients == 0 || dups[0].Match != "text" || rep.SimilarityCheck != "off" {
		t.Fatalf("duplicates = %+v", rep)
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
	rep, _ = campaign.DuplicateSends(ctx, pool, c, nil)
	dups = rep.Duplicates
	if len(dups) == 0 || dups[0].Channel != "sms" || dups[0].Match != "brief" {
		t.Fatalf("same brief, different text: %+v", dups)
	}
	// Different brief and different text: nothing.
	if _, err := pool.Exec(ctx, `update campaigns set brief = '  ANKARA için   başka bir kampanya' where id=$1`, third); err != nil {
		t.Fatal(err)
	}
	c, _ = store.Get(ctx, third)
	if rep, _ := campaign.DuplicateSends(ctx, pool, c, nil); len(rep.Duplicates) != 0 {
		t.Fatalf("unrelated campaign reported as duplicate: %+v", dups)
	}
}

// The optional LLM check sees only campaigns of the window that reached this
// audience and are not already exact matches; its verdicts carry a reason;
// its failure keeps the exact result and never blocks approval.
func TestDuplicateSimilarityCheck(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	store := campaign.NewStore(pool, "mock")
	approve := func(id uuid.UUID) {
		t.Helper()
		if _, err := campaign.Approve(ctx, pool, id, campaign.ApproveRequest{ConfirmDuplicate: true}); err != nil {
			t.Fatal(err)
		}
	}
	setBrief := func(id uuid.UUID, brief string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `update campaigns set brief=$2, channels='{sms}',
			content = jsonb_set(content, '{sms}', to_jsonb($2::text)) where id=$1`, id, brief+" {{first_name}}"); err != nil {
			t.Fatal(err)
		}
	}
	// Same audience, different wording.
	earlier := readyCampaign(t, pool, istanbulAbandoned())
	setBrief(earlier, "İstanbul'da sepetini bırakanlara %30 indirim hatırlat")
	approve(earlier)
	// Different audience (Ankara): must not be offered to the LLM.
	ankara := readyCampaign(t, pool, segment.Rule{Audience: "filtered", Conditions: []segment.Condition{
		{Field: "city", Op: "in", Value: raw([]string{"Ankara"})}}})
	setBrief(ankara, "Ankara kampanyası")
	approve(ankara)

	cur := readyCampaign(t, pool, istanbulAbandoned())
	setBrief(cur, "Sepette ürün unutan İstanbullulara yüzde otuz indirim")
	c, _ := store.Get(ctx, cur)

	var offered []campaign.SimilarCandidate
	similar := func(_ context.Context, brief string, cands []campaign.SimilarCandidate) (map[uuid.UUID]string, error) {
		offered = cands
		return map[uuid.UUID]string{earlier: "İkisi de İstanbul'da sepet terk edenlere %30 indirim."}, nil
	}
	rep, err := campaign.DuplicateSends(ctx, pool, c, similar)
	if err != nil {
		t.Fatal(err)
	}
	if len(offered) != 1 || offered[0].ID != earlier {
		t.Fatalf("offered = %+v, want only the earlier İstanbul campaign", offered)
	}
	if rep.SimilarityCheck != "ok" || len(rep.Duplicates) == 0 {
		t.Fatalf("report = %+v", rep)
	}
	for _, d := range rep.Duplicates {
		if d.CampaignID != earlier || d.Match != "similar" || d.Reason == "" {
			t.Fatalf("duplicate = %+v", d)
		}
	}
	// Approve enforces only the exact checks: no confirm needed here.
	if _, err := campaign.Approve(ctx, pool, cur, campaign.ApproveRequest{}); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// LLM failure: exact result, status "error", no error returned.
	next := readyCampaign(t, pool, istanbulAbandoned())
	setBrief(next, "Bambaşka bir yaz kampanyası")
	c, _ = store.Get(ctx, next)
	rep, err = campaign.DuplicateSends(ctx, pool, c, func(context.Context, string, []campaign.SimilarCandidate) (map[uuid.UUID]string, error) {
		return nil, errors.New("boom")
	})
	if err != nil || rep.SimilarityCheck != "error" || len(rep.Duplicates) != 0 {
		t.Fatalf("on LLM error: %+v, %v", rep, err)
	}
}
