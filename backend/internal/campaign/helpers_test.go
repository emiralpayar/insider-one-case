package campaign_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/content"
	"github.com/emiralpayar/campaign-composer/internal/dbtest"
	"github.com/emiralpayar/campaign-composer/internal/seed"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seeded(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.New(t)
	if _, err := seed.Run(context.Background(), pool, seed.Options{Users: 300, Seed: 42, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return pool
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func istanbulAbandoned() segment.Rule {
	return segment.Rule{Audience: "filtered", Conditions: []segment.Condition{
		{Field: "city", Op: "in", Value: raw([]string{"İstanbul"})},
		{Field: "abandoned_cart", Op: "within_days", Value: raw(30)},
	}, Unmapped: []string{}}
}

func goodContent() content.Content {
	return content.Content{
		SMS: "Merhaba {{first_name}}, sepetindeki ürünler seni bekliyor! %30 indirim için hemen dön.",
		Email: content.Email{
			Subject:   "Sepetin seni bekliyor",
			Preheader: "Kısa süreli fırsat",
			Body:      "Merhaba {{first_name}},\n\n{{city}} için özel %30 indirim hazırladık.",
		},
	}
}

// readyCampaign creates a content_ready campaign with the given rule.
func readyCampaign(t *testing.T, pool *pgxpool.Pool, rule segment.Rule) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	st := campaign.NewStore(pool, "mock")
	c, err := st.Create(ctx, "İstanbul'da sepette ürün bırakanlara %30 indirim")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveRule(ctx, c.ID, rule); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveContent(ctx, c.ID, goodContent()); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func userID(t *testing.T, pool *pgxpool.Pool, s seed.Scenario) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `select id from users where email=$1`, string(s)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func countMessages(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, channel string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from messages where campaign_id=$1 and ($2='' or channel=$2)`, id, channel).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
