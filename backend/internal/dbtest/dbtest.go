// Package dbtest provides isolated Postgres databases for tests.
package dbtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	mu      sync.Mutex
	urls    = map[*testing.T]string{}
	counter atomic.Int64
)

// URL returns the URL of the database most recently created by New for t.
func URL(t *testing.T) string {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	u, ok := urls[t]
	if !ok {
		t.Fatal("dbtest.URL called before dbtest.New")
	}
	return u
}

// New creates a fresh, migrated database for this test and returns a pool.
// Each call yields a new database; it is dropped on cleanup.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("TEST_DATABASE_URL is not set")
		}
		t.Skip("TEST_DATABASE_URL is not set (run `make test`)")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer admin.Close(ctx)

	name := fmt.Sprintf("t_%d_%d", time.Now().UnixNano(), counter.Add(1))
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	dbURL := u.String()

	if err := db.Migrate(ctx, dbURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := db.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	mu.Lock()
	urls[t] = dbURL
	mu.Unlock()

	t.Cleanup(func() {
		mu.Lock()
		delete(urls, t)
		mu.Unlock()
		pool.Close()
		c, err := pgx.Connect(ctx, base)
		if err != nil {
			return
		}
		defer c.Close(ctx)
		_, _ = c.Exec(ctx, "drop database if exists "+name+" with (force)")
	})
	return pool
}
