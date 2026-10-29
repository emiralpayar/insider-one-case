package campaign_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/db"
	"github.com/emiralpayar/campaign-composer/internal/dbtest"
	"github.com/emiralpayar/campaign-composer/internal/seed"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/emiralpayar/campaign-composer/internal/verr"
	"github.com/google/uuid"
)

func TestApproveEnqueuesOnlyConsented(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	id := readyCampaign(t, pool, istanbulAbandoned())
	// Make the empty-name scenario user opt out of email (keeps SMS consent).
	emptyName := userID(t, pool, seed.ScenarioEmptyName)
	if _, err := pool.Exec(ctx, `update users set email_consent=false where id=$1`, emptyName); err != nil {
		t.Fatal(err)
	}

	res, err := campaign.Approve(ctx, pool, id, campaign.ApproveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var noEmail int
	pool.QueryRow(ctx, `select count(*) from messages where campaign_id=$1 and user_id=$2 and channel='email'`, id, emptyName).Scan(&noEmail)
	if noEmail != 0 {
		t.Fatalf("user without email consent got %d email rows", noEmail)
	}
	var unconsented int
	pool.QueryRow(ctx, `select count(*) from messages m join users u on u.id=m.user_id
		where m.campaign_id=$1 and ((m.channel='email' and u.email_consent is not true) or (m.channel='sms' and u.sms_consent is not true))`,
		id).Scan(&unconsented)
	if unconsented != 0 {
		t.Fatalf("%d messages queued without consent", unconsented)
	}
	if res.Enqueued["sms"] == 0 || res.Enqueued["email"] == 0 {
		t.Fatalf("enqueued = %v", res.Enqueued)
	}
	if got := countMessages(t, pool, id, "sms"); got != res.Enqueued["sms"] {
		t.Fatalf("sms rows %d != enqueued %d", got, res.Enqueued["sms"])
	}

	noSMS := userID(t, pool, seed.ScenarioNoSMSConsent)
	var n int
	pool.QueryRow(ctx, `select count(*) from messages where campaign_id=$1 and user_id=$2 and channel='sms'`, id, noSMS).Scan(&n)
	if n != 0 {
		t.Fatalf("no-sms-consent user got %d sms rows", n)
	}
	var recipient, subject string
	if err := pool.QueryRow(ctx, `select recipient, subject from messages where campaign_id=$1 and user_id=$2 and channel='email'`,
		id, noSMS).Scan(&recipient, &subject); err != nil {
		t.Fatalf("no-sms-consent user email row: %v", err)
	}
	if recipient != string(seed.ScenarioNoSMSConsent) || subject != "Sepetin seni bekliyor" {
		t.Fatalf("email row recipient=%q subject=%q", recipient, subject)
	}

	var body, status string
	var attempts int
	var smsSubject *string
	if err := pool.QueryRow(ctx, `select body, status, attempts, subject from messages where campaign_id=$1 and user_id=$2 and channel='sms'`,
		id, userID(t, pool, seed.ScenarioEmptyName)).Scan(&body, &status, &attempts, &smsSubject); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body, "Merhaba, ") {
		t.Fatalf("empty-name sms body = %q", body)
	}
	if status != "queued" || attempts != 0 || smsSubject != nil {
		t.Fatalf("status=%q attempts=%d subject=%v", status, attempts, smsSubject)
	}

	c, err := campaign.NewStore(pool, "mock").Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "sending" || c.ApprovedAt == nil {
		t.Fatalf("campaign status=%q approved_at=%v", c.Status, c.ApprovedAt)
	}
}

func TestApproveConcurrentOnce(t *testing.T) {
	pool := seeded(t)
	id := readyCampaign(t, pool, istanbulAbandoned())

	var wg sync.WaitGroup
	results := make([]campaign.ApproveResult, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = campaign.Approve(context.Background(), pool, id, campaign.ApproveRequest{})
		}()
	}
	close(start)
	wg.Wait()

	ok, already := -1, -1
	for i, err := range errs {
		switch {
		case err == nil:
			ok = i
		case errors.Is(err, campaign.ErrAlreadyApproved):
			already = i
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok < 0 || already < 0 {
		t.Fatalf("want one success and one ErrAlreadyApproved, got %v", errs)
	}
	want := results[ok].Enqueued["sms"] + results[ok].Enqueued["email"]
	if got := countMessages(t, pool, id, ""); got != want || got == 0 {
		t.Fatalf("messages = %d, want %d", got, want)
	}
}

func TestApproveLargeAudienceNeedsConfirm(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	id := readyCampaign(t, pool, segment.Rule{Audience: "all", Conditions: []segment.Condition{}, Unmapped: []string{}})

	if _, err := campaign.Approve(ctx, pool, id, campaign.ApproveRequest{}); !errors.Is(err, campaign.ErrLargeAudienceNotConfirmed) {
		t.Fatalf("err = %v, want ErrLargeAudienceNotConfirmed", err)
	}
	if n := countMessages(t, pool, id, ""); n != 0 {
		t.Fatalf("messages after rejected approve = %d", n)
	}
	res, err := campaign.Approve(ctx, pool, id, campaign.ApproveRequest{ConfirmLargeAudience: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Enqueued["sms"] < 150 {
		t.Fatalf("enqueued = %v", res.Enqueued)
	}
}

func TestApproveEmptyAudience(t *testing.T) {
	pool := seeded(t)
	rule := segment.Rule{Audience: "filtered", Conditions: []segment.Condition{
		{Field: "order_count", Op: "gte", Value: raw(1000)},
	}, Unmapped: []string{}}
	id := readyCampaign(t, pool, rule)
	if _, err := campaign.Approve(context.Background(), pool, id, campaign.ApproveRequest{ConfirmLargeAudience: true}); !errors.Is(err, campaign.ErrEmptyAudience) {
		t.Fatalf("err = %v, want ErrEmptyAudience", err)
	}
}

func TestApproveRevalidatesContent(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	id := readyCampaign(t, pool, istanbulAbandoned())
	if _, err := pool.Exec(ctx, `update campaigns set content = jsonb_set(content, '{sms}', to_jsonb(repeat('a', 200))) where id=$1`, id); err != nil {
		t.Fatal(err)
	}
	_, err := campaign.Approve(ctx, pool, id, campaign.ApproveRequest{})
	var vl verr.List
	if !errors.As(err, &vl) || len(vl) == 0 || vl[0].Code != "sms_too_long" {
		t.Fatalf("err = %v, want verr.List with sms_too_long", err)
	}
	if n := countMessages(t, pool, id, ""); n != 0 {
		t.Fatalf("messages = %d", n)
	}
	var status string
	pool.QueryRow(ctx, `select status from campaigns where id=$1`, id).Scan(&status)
	if status != "content_ready" {
		t.Fatalf("status = %q", status)
	}
}

func TestApproveRespectsChannels(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	id := readyCampaign(t, pool, istanbulAbandoned())
	if err := campaign.NewStore(pool, "mock").SetChannels(ctx, id, []string{"sms"}); err != nil {
		t.Fatal(err)
	}
	res, err := campaign.Approve(ctx, pool, id, campaign.ApproveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Enqueued["email"]; ok {
		t.Fatalf("enqueued = %v", res.Enqueued)
	}
	if n := countMessages(t, pool, id, "email"); n != 0 {
		t.Fatalf("email rows = %d", n)
	}
	if countMessages(t, pool, id, "sms") == 0 {
		t.Fatal("no sms rows")
	}
}

func TestApproveNotReady(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	st := campaign.NewStore(pool, "mock")
	c, err := st.Create(ctx, "brief")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := campaign.Approve(ctx, pool, c.ID, campaign.ApproveRequest{}); !errors.Is(err, campaign.ErrNotReady) {
		t.Fatalf("err = %v, want ErrNotReady", err)
	}
}

func TestStoreTransitions(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	st := campaign.NewStore(pool, "mock")
	c, err := st.Create(ctx, "brief")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "draft" || c.LLMProvider != "mock" || len(c.Channels) != 2 {
		t.Fatalf("created = %+v", c)
	}
	if err := st.SaveContent(ctx, c.ID, goodContent()); !errors.Is(err, campaign.ErrNotReady) {
		t.Fatalf("SaveContent on draft: %v", err)
	}
	if err := st.SaveRule(ctx, c.ID, istanbulAbandoned()); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(ctx, c.ID); got.Status != "audience_ready" || got.Rule == nil {
		t.Fatalf("after SaveRule: %+v", got)
	}
	if err := st.SaveContent(ctx, c.ID, goodContent()); err != nil {
		t.Fatal(err)
	}
	// Changing the rule keeps existing content and content_ready.
	if err := st.SaveRule(ctx, c.ID, istanbulAbandoned()); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(ctx, c.ID); got.Status != "content_ready" || got.Content == nil {
		t.Fatalf("after second SaveRule: %+v", got)
	}
	if err := st.SetChannels(ctx, c.ID, []string{"fax"}); err == nil {
		t.Fatal("SetChannels accepted fax")
	}
	if err := st.SetChannels(ctx, c.ID, []string{}); err == nil {
		t.Fatal("SetChannels accepted empty")
	}
	if _, err := campaign.Approve(ctx, pool, c.ID, campaign.ApproveRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveRule(ctx, c.ID, istanbulAbandoned()); !errors.Is(err, campaign.ErrLocked) {
		t.Fatalf("SaveRule after approve: %v", err)
	}
	if err := st.SaveContent(ctx, c.ID, goodContent()); !errors.Is(err, campaign.ErrLocked) {
		t.Fatalf("SaveContent after approve: %v", err)
	}
	if _, err := st.Get(ctx, [16]byte{1}); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("Get missing: %v", err)
	}
	list, err := st.List(ctx, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
}

func TestWorstCaseFor(t *testing.T) {
	pool := seeded(t)
	wc, err := campaign.WorstCaseFor(context.Background(), pool, istanbulAbandoned(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if wc.LongestFirstName != "Abdurrahman Muhammed" || wc.LongestCity != "İstanbul" {
		t.Fatalf("wc = %+v", wc)
	}
}

// TestApproveSmallPoolNoDeadlock: Approve must use a single connection while
// it holds the campaign lock, so a burst of approvals on a 4-connection pool
// (8 distinct campaigns + 8 on one campaign) finishes.
func TestApproveSmallPoolNoDeadlock(t *testing.T) {
	big := seeded(t)
	ctx := context.Background()
	ids := make([]uuid.UUID, 8)
	for i := range ids {
		ids[i] = readyCampaign(t, big, istanbulAbandoned())
	}
	small, err := db.Open(ctx, dbtest.URL(t)+"&pool_max_conns=4")
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()

	type result struct {
		id  uuid.UUID
		err error
	}
	targets := append(append([]uuid.UUID{}, ids...), repeatID(ids[0], 8)...)
	results := make(chan result, len(targets))
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	for _, id := range targets {
		go func() {
			<-start
			_, err := campaign.Approve(tctx, small, id, campaign.ApproveRequest{})
			results <- result{id, err}
		}()
	}
	close(start)
	success := map[uuid.UUID]int{}
	for range targets {
		select {
		case r := <-results:
			switch {
			case r.err == nil:
				success[r.id]++
			case errors.Is(r.err, campaign.ErrAlreadyApproved):
			default:
				t.Fatalf("approve %s: %v", r.id, r.err)
			}
		case <-time.After(35 * time.Second):
			t.Fatal("approvals hung (pool deadlock)")
		}
	}
	for _, id := range ids {
		if success[id] != 1 {
			t.Fatalf("campaign %s: %d successes, want 1", id, success[id])
		}
	}
}

func repeatID(id uuid.UUID, n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = id
	}
	return out
}
