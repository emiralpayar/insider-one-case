package seed_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/dbtest"
	"github.com/emiralpayar/campaign-composer/internal/seed"
	"github.com/jackc/pgx/v5/pgxpool"
)

var fixedNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func count(t *testing.T, pool *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSeedCountsAndIdempotent(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	res, err := seed.Run(ctx, pool, seed.Options{Users: 5000, Seed: 42, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if res.Users != 5000 || res.Events <= 0 || res.Skipped {
		t.Fatalf("unexpected result %+v", res)
	}
	res2, err := seed.Run(ctx, pool, seed.Options{Users: 5000, Seed: 42, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Skipped {
		t.Fatal("second run should be skipped")
	}
	if n := count(t, pool, "select count(*) from users"); n != 5000 {
		t.Fatalf("users = %d", n)
	}
}

func TestSeedDeterministic(t *testing.T) {
	ctx := context.Background()
	list := func() []string {
		pool := dbtest.New(t)
		if _, err := seed.Run(ctx, pool, seed.Options{Users: 500, Seed: 7, Now: fixedNow}); err != nil {
			t.Fatal(err)
		}
		rows, err := pool.Query(ctx, "select first_name, city, email from users order by id limit 50")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var a, b, c string
			if err := rows.Scan(&a, &b, &c); err != nil {
				t.Fatal(err)
			}
			out = append(out, a+"|"+b+"|"+c)
		}
		return out
	}
	a, b := list(), list()
	if len(a) != 50 || len(a) != len(b) {
		t.Fatalf("lens %d %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("row %d differs: %q vs %q", i, a[i], b[i])
		}
	}
}

func TestSeedDistribution(t *testing.T) {
	pool := dbtest.New(t)
	if _, err := seed.Run(context.Background(), pool, seed.Options{Users: 5000, Seed: 42, Now: fixedNow}); err != nil {
		t.Fatal(err)
	}
	pct := func(where string) float64 {
		return 100 * float64(count(t, pool, "select count(*) from users where "+where)) / 5000
	}
	checks := []struct {
		name, where string
		lo, hi      float64
	}{
		{"istanbul", "city = 'İstanbul'", 25, 45},
		{"empty first_name", "first_name = ''", 3, 8},
		{"en", "language = 'en'", 5, 15},
		{"sms", "sms_consent", 70, 90},
		{"email", "email_consent", 75, 95},
	}
	for _, c := range checks {
		if p := pct(c.where); p < c.lo || p > c.hi {
			t.Errorf("%s = %.1f%%, want %v-%v", c.name, p, c.lo, c.hi)
		}
	}
}

func TestSeedScenarios(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	if _, err := seed.Run(ctx, pool, seed.Options{Users: 5000, Seed: 42, Now: fixedNow}); err != nil {
		t.Fatal(err)
	}
	type ev struct {
		typ  string
		days int
	}
	cases := []struct {
		name   seed.Scenario
		city   string
		events []ev
	}{
		{seed.ScenarioAbandonedAfterPurchase, "İstanbul", []ev{{"purchase", -20}, {"cart_add", -5}}},
		{seed.ScenarioPurchasedAfterCart, "İstanbul", []ev{{"cart_add", -10}, {"purchase", -8}}},
		{seed.ScenarioAnkaraAbandoned, "Ankara", []ev{{"cart_add", -3}}},
		{seed.ScenarioOldCart, "İstanbul", []ev{{"cart_add", -40}}},
		{seed.ScenarioNoSMSConsent, "İstanbul", []ev{{"cart_add", -2}}},
		{seed.ScenarioEmptyName, "İstanbul", []ev{{"cart_add", -4}}},
		{seed.ScenarioLongestName, "İstanbul", []ev{{"cart_add", -6}}},
	}
	for _, c := range cases {
		t.Run(string(c.name), func(t *testing.T) {
			var id int64
			var city, first string
			var sms, email *bool
			err := pool.QueryRow(ctx, "select id, city, first_name, sms_consent, email_consent from users where email=$1", string(c.name)).
				Scan(&id, &city, &first, &sms, &email)
			if err != nil {
				t.Fatalf("user not found: %v", err)
			}
			if city != c.city {
				t.Errorf("city %q", city)
			}
			rows, err := pool.Query(ctx, "select type, occurred_at from events where user_id=$1 order by occurred_at", id)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			got := map[string]float64{}
			n := 0
			for rows.Next() {
				var typ string
				var at time.Time
				if err := rows.Scan(&typ, &at); err != nil {
					t.Fatal(err)
				}
				got[typ] = at.Sub(fixedNow).Hours() / 24
				n++
			}
			if n != len(c.events) {
				t.Fatalf("events = %d, want %d", n, len(c.events))
			}
			for _, e := range c.events {
				if math.Abs(got[e.typ]-float64(e.days)) > 1e-6 {
					t.Errorf("%s at %v days, want %d", e.typ, got[e.typ], e.days)
				}
			}
			switch c.name {
			case seed.ScenarioNoSMSConsent:
				if sms == nil || *sms || email == nil || !*email {
					t.Errorf("consents sms=%v email=%v", sms, email)
				}
			case seed.ScenarioEmptyName:
				if first != "" {
					t.Errorf("first_name %q", first)
				}
			case seed.ScenarioLongestName:
				if first != "Abdurrahman Muhammed" {
					t.Errorf("first_name %q", first)
				}
			}
		})
	}
}
