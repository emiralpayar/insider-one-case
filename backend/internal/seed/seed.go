// Package seed generates deterministic demo users and events.
package seed

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/geo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options configures a seed run. Zero values: Users 5000, Seed 42, Now time.Now().
type Options struct {
	Users int
	Seed  int64
	Now   time.Time
}

// Result reports what Run did.
type Result struct {
	Users, Events int
	Skipped       bool
}

const day = 24 * time.Hour

// Run seeds users and events unless users already exist.
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) (Result, error) {
	if opts.Users == 0 {
		opts.Users = 5000
	}
	if opts.Seed == 0 {
		opts.Seed = 42
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Users < len(scenarioDefs) {
		return Result{}, fmt.Errorf("seed: users must be at least %d", len(scenarioDefs))
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)

	// Serialize concurrent seeders, then check emptiness.
	if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock(424242)"); err != nil {
		return Result{}, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, "select exists(select 1 from users)").Scan(&exists); err != nil {
		return Result{}, err
	}
	if exists {
		return Result{Skipped: true}, nil
	}

	r := rand.New(rand.NewPCG(uint64(opts.Seed), uint64(opts.Seed)))
	var userRows, eventRows [][]any
	now := opts.Now
	randomCount := opts.Users - len(scenarioDefs)

	for i := 1; i <= randomCount; i++ {
		en := r.Float64() < 0.10
		first, last := pick(r, trFirstNames), pick(r, trLastNames)
		if en {
			first, last = pick(r, enFirstNames), pick(r, enLastNames)
		}
		if r.Float64() < 0.055 {
			first = ""
		}
		lang := "tr"
		if en {
			lang = "en"
		}
		consentAt := now.Add(-time.Duration(r.IntN(365*24)) * time.Hour)
		userRows = append(userRows, []any{
			int64(i), first, last, fmt.Sprintf("user%d@example.test", i), phone(r), pickCity(r), lang,
			r.Float64() < 0.80, r.Float64() < 0.85, consentAt, now.Add(-time.Duration(180+r.IntN(500)) * day),
		})
		eventRows = append(eventRows, randomEvents(r, int64(i), now)...)
	}

	for j, s := range scenarioDefs {
		id := int64(randomCount + j + 1)
		userRows = append(userRows, []any{
			id, s.firstName, "Test", string(s.email), phone(r), s.city, "tr",
			s.smsConsent, true, now.Add(-90 * day), now.Add(-200 * day),
		})
		for _, e := range s.events {
			var amount any
			if e.typ == "purchase" {
				amount = 250.0
			} else {
				amount = 120.0
			}
			eventRows = append(eventRows, []any{id, e.typ, now.Add(-time.Duration(e.daysAgo) * day), amount})
		}
	}

	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"users"},
		[]string{"id", "first_name", "last_name", "email", "phone", "city", "language", "sms_consent", "email_consent", "consent_updated_at", "created_at"},
		pgx.CopyFromRows(userRows)); err != nil {
		return Result{}, fmt.Errorf("copy users: %w", err)
	}
	if _, err := tx.Exec(ctx, "select setval(pg_get_serial_sequence('users','id'), (select max(id) from users))"); err != nil {
		return Result{}, err
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"events"},
		[]string{"user_id", "type", "occurred_at", "amount"}, pgx.CopyFromRows(eventRows)); err != nil {
		return Result{}, fmt.Errorf("copy events: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{Users: len(userRows), Events: len(eventRows)}, nil
}

// randomEvents makes 0-8 events within the last 120 days; ~60% of purchases
// follow a cart_add by 0-3 days.
func randomEvents(r *rand.Rand, userID int64, now time.Time) [][]any {
	n := r.IntN(9)
	var out [][]any
	for k := 0; k < n; k++ {
		at := now.Add(-time.Duration(r.Int64N(int64(120 * day))))
		if r.Float64() < 0.65 {
			out = append(out, []any{userID, "cart_add", at, float64(50 + r.IntN(450))})
			continue
		}
		if r.Float64() < 0.6 {
			cart := at.Add(-time.Duration(r.Int64N(int64(3 * day))))
			out = append(out, []any{userID, "cart_add", cart, float64(50 + r.IntN(450))})
		}
		out = append(out, []any{userID, "purchase", at, float64(50 + r.IntN(450))})
	}
	return out
}

func pick(r *rand.Rand, s []string) string { return s[r.IntN(len(s))] }

func phone(r *rand.Rand) string { return fmt.Sprintf("+905%09d", r.IntN(1_000_000_000)) }

func pickCity(r *rand.Rand) string {
	x := r.Float64()
	var acc float64
	for _, c := range geo.Cities {
		acc += c.Weight
		if x < acc {
			return c.Name
		}
	}
	return geo.Cities[0].Name
}
