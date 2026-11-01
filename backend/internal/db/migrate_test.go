package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/db"
	"github.com/emiralpayar/campaign-composer/internal/dbtest"
	"github.com/google/uuid"
)

// The 00002 backfill names existing campaigns with the same rule as
// campaign.DefaultName.
func TestCampaignNameBackfill(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	url := dbtest.URL(t)
	if err := db.MigrateDownTo(ctx, url, 1); err != nil {
		t.Fatal(err)
	}
	pool.Reset() // the column set changed under pooled connections
	briefs := []string{
		"Kısa brief",
		"  çok   boşluklu\n\nbrief  ",
		"Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder.",
		strings.Repeat("abcd ", 12) + "xyz",
		strings.Repeat("ş", 70),
		strings.Repeat("x", 55) + ", " + strings.Repeat("y", 10),
		strings.Repeat("a", 60),
	}
	ids := make([]uuid.UUID, len(briefs))
	for i, b := range briefs {
		ids[i] = uuid.New()
		if _, err := pool.Exec(ctx, `insert into campaigns (id, brief, llm_provider, status) values ($1, $2, 'mock', 'draft')`, ids[i], b); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool.Reset()
	for i, b := range briefs {
		var name string
		if err := pool.QueryRow(ctx, `select name from campaigns where id=$1`, ids[i]).Scan(&name); err != nil {
			t.Fatal(err)
		}
		if want := campaign.DefaultName(b); name != want {
			t.Errorf("backfill(%q) = %q, Go rule gives %q", b, name, want)
		}
	}
}
