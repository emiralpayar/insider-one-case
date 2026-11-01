package segment_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/emiralpayar/campaign-composer/internal/seed"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/emiralpayar/campaign-composer/internal/verr"
)

func TestMembersMatchPreviewAndPage(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	rule := filtered(cond("city", "in", []string{"İstanbul", "Ankara"}))
	pv, err := segment.Preview(ctx, pool, rule, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := segment.MatchIDs(ctx, pool, rule, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	first, err := segment.Members(ctx, pool, rule, seedNow, segment.MemberQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != pv.Total || first.Page != 1 || first.PerPage != segment.MembersPerPage ||
		len(first.Members) != min(pv.Total, segment.MembersPerPage) {
		t.Fatalf("page 1: total=%d len=%d (preview %d)", first.Total, len(first.Members), pv.Total)
	}

	// Paging in 20s returns exactly MatchIDs, in id order.
	var got []int64
	for p := 1; ; p++ {
		pg, err := segment.Members(ctx, pool, rule, seedNow, segment.MemberQuery{Page: p, PerPage: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(pg.Members) == 0 {
			break
		}
		for _, m := range pg.Members {
			if m.City != "İstanbul" && m.City != "Ankara" {
				t.Fatalf("member outside the rule: %+v", m)
			}
			got = append(got, m.ID)
		}
	}
	if len(got) != len(ids) {
		t.Fatalf("paged %d, MatchIDs %d", len(got), len(ids))
	}
	for i := range ids {
		if got[i] != ids[i] {
			t.Fatalf("order differs at %d: %d vs %d", i, got[i], ids[i])
		}
	}
}

func TestMembersStatsAndConsent(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	id := idOf(t, pool, seed.ScenarioNoSMSConsent)
	var orders int
	var spent float64
	if err := pool.QueryRow(ctx, `select count(*), coalesce(sum(amount),0)::float8 from events where user_id=$1 and type='purchase'`, id).
		Scan(&orders, &spent); err != nil {
		t.Fatal(err)
	}
	pg, err := segment.Members(ctx, pool, istanbulAbandoned(), seedNow, segment.MemberQuery{PerPage: 200})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range pg.Members {
		if m.ID != id {
			continue
		}
		if m.SMSConsent == nil || *m.SMSConsent || m.OrderCount != orders || m.TotalSpent != spent || m.Language == "" {
			t.Fatalf("member = %+v (orders %d spent %v)", m, orders, spent)
		}
		return
	}
	t.Fatalf("scenario user %d not in the audience", id)
}

func TestMembersSearch(t *testing.T) {
	pool := seeded(t)
	ctx := context.Background()
	all := segment.Rule{Audience: "all", Conditions: []segment.Condition{}, Unmapped: []string{}}
	var first, last string
	if err := pool.QueryRow(ctx, `select first_name, last_name from users where first_name ~ '^[A-Za-z]+$' and last_name is not null order by id limit 1`).
		Scan(&first, &last); err != nil {
		t.Fatal(err)
	}
	pg, err := segment.Members(ctx, pool, all, seedNow, segment.MemberQuery{Search: "  " + strings.ToLower(first) + " "})
	if err != nil {
		t.Fatal(err)
	}
	if pg.Total == 0 {
		t.Fatalf("search %q found nobody", first)
	}
	for _, m := range pg.Members {
		if !strings.Contains(strings.ToLower(m.FirstName+" "+m.LastName), strings.ToLower(first)) {
			t.Fatalf("%+v does not match %q", m, first)
		}
	}
	full, err := segment.Members(ctx, pool, all, seedNow, segment.MemberQuery{Search: first + " " + last})
	if err != nil || full.Total == 0 || full.Total > pg.Total {
		t.Fatalf("full name: %d (first name %d) %v", full.Total, pg.Total, err)
	}

	// LIKE wildcards and quotes are data, not pattern or SQL.
	for _, s := range []string{"%", "_", `\`, "'; drop table users; --"} {
		pg, err := segment.Members(ctx, pool, all, seedNow, segment.MemberQuery{Search: s})
		if err != nil || pg.Total != 0 {
			t.Fatalf("search %q: total %d err %v", s, pg.Total, err)
		}
	}
	var vl verr.List
	if _, err := segment.Members(ctx, pool, all, seedNow, segment.MemberQuery{Search: strings.Repeat("a", 101)}); !errors.As(err, &vl) {
		t.Fatalf("long search: %v", err)
	}
	// An invalid rule is rejected by Compile.
	if _, err := segment.Members(ctx, pool, filtered(cond("phone", "eq", "1")), seedNow, segment.MemberQuery{}); err == nil {
		t.Fatal("invalid rule accepted")
	}
}
