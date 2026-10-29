package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/dbtest"
	"github.com/emiralpayar/campaign-composer/internal/seed"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newDB returns a migrated test database seeded with n users.
func newDB(t *testing.T, users int) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.New(t)
	if _, err := seed.Run(context.Background(), pool, seed.Options{Users: users, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return pool
}

func insertCampaign(t *testing.T, pool *pgxpool.Pool, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`insert into campaigns (id, brief, llm_provider, status) values ($1, 'test', 'mock', $2)`, id, status)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// insertMessages queues one message per user (the first n users by id) on the
// channel, the way the approval step will. Consent is not filtered here.
func insertMessages(t *testing.T, pool *pgxpool.Pool, campaign uuid.UUID, channel string, n int) []uuid.UUID {
	t.Helper()
	recipient := "phone"
	if channel == "email" {
		recipient = "email"
	}
	rows, err := pool.Query(context.Background(),
		`insert into messages (id, campaign_id, user_id, channel, recipient, subject, body, status)
		 select gen_random_uuid(), $1, u.id, $2, u.`+recipient+`, case when $2='email' then 'Hi' end,
		        'Hello '||u.first_name, 'queued'
		 from (select * from users order by id limit $3) u
		 returning id`, campaign, channel, n)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

type msgRow struct {
	Status   string
	Attempts int
	LastErr  *string
}

func getMessage(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) msgRow {
	t.Helper()
	var r msgRow
	if err := pool.QueryRow(context.Background(),
		`select status, attempts, last_error from messages where id=$1`, id).Scan(&r.Status, &r.Attempts, &r.LastErr); err != nil {
		t.Fatal(err)
	}
	return r
}

func statusCounts(t *testing.T, pool *pgxpool.Pool, campaign uuid.UUID) map[string]int {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`select status, count(*) from messages where campaign_id=$1 group by status`, campaign)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			t.Fatal(err)
		}
		out[s] = n
	}
	return out
}

// waitFor polls cond every 50ms until it returns true or timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// countingProvider records provider calls per message.
type countingProvider struct {
	Provider
	mu    sync.Mutex
	calls map[uuid.UUID]int
	total int
}

func newCounting(p Provider) *countingProvider {
	return &countingProvider{Provider: p, calls: map[uuid.UUID]int{}}
}

func (c *countingProvider) Send(ctx context.Context, m OutboundMessage) SendResult {
	c.mu.Lock()
	c.calls[m.ID]++
	c.total++
	c.mu.Unlock()
	return c.Provider.Send(ctx, m)
}

func (c *countingProvider) Total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

func (c *countingProvider) MinCalls(ids []uuid.UUID) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	min := -1
	for _, id := range ids {
		if n := c.calls[id]; min < 0 || n < min {
			min = n
		}
	}
	return min
}
