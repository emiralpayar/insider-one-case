package segment_test

import (
	"context"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/dbtest"
	"github.com/emiralpayar/campaign-composer/internal/seed"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/jackc/pgx/v5/pgxpool"
)

var seedNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func seeded(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.New(t)
	if _, err := seed.Run(context.Background(), pool, seed.Options{Users: 300, Seed: 42, Now: seedNow}); err != nil {
		t.Fatal(err)
	}
	return pool
}

func idOf(t *testing.T, pool *pgxpool.Pool, s seed.Scenario) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `select id from users where email=$1`, string(s)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func istanbulAbandoned() segment.Rule {
	return filtered(cond("city", "in", []string{"İstanbul"}), cond("abandoned_cart", "within_days", 30))
}

func TestAbandonedCartSemantics(t *testing.T) {
	pool := seeded(t)
	ids, err := segment.MatchIDs(context.Background(), pool, istanbulAbandoned(), seedNow)
	if err != nil {
		t.Fatal(err)
	}
	set := map[int64]bool{}
	for _, id := range ids {
		set[id] = true
	}
	in := []seed.Scenario{seed.ScenarioAbandonedAfterPurchase, seed.ScenarioNoSMSConsent, seed.ScenarioEmptyName, seed.ScenarioLongestName}
	out := []seed.Scenario{seed.ScenarioPurchasedAfterCart, seed.ScenarioAnkaraAbandoned, seed.ScenarioOldCart}
	for _, s := range in {
		if !set[idOf(t, pool, s)] {
			t.Errorf("%s should match", s)
		}
	}
	for _, s := range out {
		if set[idOf(t, pool, s)] {
			t.Errorf("%s should not match", s)
		}
	}
}

func TestPreviewEligibility(t *testing.T) {
	pool := seeded(t)
	p, err := segment.Preview(context.Background(), pool, istanbulAbandoned(), seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total == 0 || p.SMSEligible >= p.Total || p.AllUsers < p.Total {
		t.Fatalf("%+v", p)
	}
	if len(p.Sample) == 0 || len(p.Sample) > 5 {
		t.Fatalf("sample %d", len(p.Sample))
	}
	for i := 1; i < len(p.Sample); i++ {
		if p.Sample[i-1].ID >= p.Sample[i].ID {
			t.Fatal("sample not ordered")
		}
	}
}

func warnCodes(p segment.PreviewResult) map[string]bool {
	m := map[string]bool{}
	for _, w := range p.Warnings {
		m[w.Code] = true
	}
	return m
}

func TestPreviewWarnings(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	p, err := segment.Preview(ctx, pool, segment.Rule{Audience: "all"}, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if c := warnCodes(p); !c["large_audience"] || !c["mixed_language"] {
		t.Fatalf("all: %+v", p.Warnings)
	}
	p, err = segment.Preview(ctx, pool, filtered(cond("order_count", "gte", 1000)), seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !warnCodes(p)["empty_audience"] || p.Total != 0 {
		t.Fatalf("empty: %+v", p)
	}
	r := istanbulAbandoned()
	r.Unmapped = []string{"VIP"}
	p, err = segment.Preview(ctx, pool, r, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !warnCodes(p)["unmapped"] {
		t.Fatalf("unmapped: %+v", p.Warnings)
	}
}

func TestOtherFieldsExecute(t *testing.T) {
	pool := seeded(t)
	r := filtered(cond("language", "eq", "tr"), cond("city", "not_in", []string{"Ankara"}),
		cond("purchased", "within_days", 90), cond("order_count", "gte", 1), cond("order_count", "lte", 50),
		cond("total_spent", "gte", 10), cond("total_spent", "lte", 100000), cond("signed_up", "within_days", 3650))
	if _, err := segment.MatchIDs(context.Background(), pool, r, seedNow); err != nil {
		t.Fatal(err)
	}
	r = filtered(cond("purchased", "not_within_days", 30))
	if _, err := segment.MatchIDs(context.Background(), pool, r, seedNow); err != nil {
		t.Fatal(err)
	}
}

func TestAbandonedCartIgnoresEventsAfterAsOf(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	var id int64
	err := pool.QueryRow(ctx, `insert into users(first_name,email,phone,city,language) values('Fut','future@example.test','+900000000000','Kayseri','tr') returning id`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into events(user_id,type,occurred_at,amount) values($1,'cart_add',$2,null),($1,'purchase',$3,10)`,
		id, seedNow.AddDate(0, 0, -3), seedNow.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	ids, err := segment.MatchIDs(ctx, pool, filtered(cond("city", "in", []string{"Kayseri"}), cond("abandoned_cart", "within_days", 30)), seedNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range ids {
		if x == id {
			return
		}
	}
	t.Fatal("user with purchase after asOf should still be abandoned at asOf")
}
