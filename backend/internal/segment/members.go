package segment

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emiralpayar/campaign-composer/internal/verr"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MembersPerPage is the default page size of the audience list.
const MembersPerPage = 50

// MaxPage bounds page numbers of the paginated lists.
const MaxPage = 100000

// maxSearchRunes bounds the name search.
const maxSearchRunes = 100

// MemberQuery pages (and optionally name-searches) a rule's audience. Page
// starts at 1.
type MemberQuery struct {
	Page    int
	PerPage int
	Search  string
}

// Member is one user of an audience, with the fields the audience list shows.
type Member struct {
	ID           int64   `json:"id"`
	FirstName    string  `json:"first_name"`
	LastName     string  `json:"last_name"`
	City         string  `json:"city"`
	Language     string  `json:"language"`
	SMSConsent   *bool   `json:"sms_consent"`
	EmailConsent *bool   `json:"email_consent"`
	OrderCount   int     `json:"order_count"`
	TotalSpent   float64 `json:"total_spent"`
}

// MemberPage is a page of the audience; Total counts the users matching the
// rule and the search.
type MemberPage struct {
	Members []Member `json:"members"`
	Total   int      `json:"total"`
	Page    int      `json:"page"`
	PerPage int      `json:"per_page"`
}

// Normalize trims the search and fills page defaults (page 1, 50 per page, at
// most 200).
func (q MemberQuery) Normalize() (MemberQuery, verr.List) {
	q.Search = strings.Join(strings.Fields(q.Search), " ")
	if utf8.RuneCountInString(q.Search) > maxSearchRunes {
		return q, verr.List{{Path: "search", Code: "too_long", Message: "Arama en fazla 100 karakter olabilir"}}
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Page > MaxPage { // (Page-1)*PerPage must not overflow the OFFSET
		return q, verr.List{{Path: "page", Code: "out_of_range", Message: fmt.Sprintf("Sayfa en fazla %d olabilir", MaxPage)}}
	}
	if q.PerPage < 1 || q.PerPage > 200 {
		q.PerPage = MembersPerPage
	}
	return q, nil
}

// likePattern turns a search into a "contains" ILIKE pattern with the LIKE
// wildcards escaped (the escape character is the default backslash).
func likePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// Members lists the users matching the rule, ordered by id. The rule is
// compiled exactly as for Preview; the search is a bind parameter too.
func Members(ctx context.Context, pool *pgxpool.Pool, r Rule, asOf time.Time, q MemberQuery) (MemberPage, error) {
	q, errs := q.Normalize()
	if errs != nil {
		return MemberPage{}, errs
	}
	where, args, err := Compile(r, 0, asOf)
	if err != nil {
		return MemberPage{}, err
	}
	page := MemberPage{Members: []Member{}, Page: q.Page, PerPage: q.PerPage}
	n := len(args)
	filter := fmt.Sprintf(`(%s) AND ($%d::text = '' OR (u.first_name || ' ' || coalesce(u.last_name, '')) ILIKE $%d::text)`,
		where, n+1, n+2)
	args = append(args, q.Search, likePattern(q.Search))

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users u WHERE `+filter, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := pool.Query(ctx, fmt.Sprintf(`SELECT u.id, u.first_name, coalesce(u.last_name, ''), u.city, u.language,
			u.sms_consent, u.email_consent,
			coalesce(o.n, 0), coalesce(o.spent, 0)::float8
		FROM users u
		LEFT JOIN LATERAL (SELECT count(*) AS n, sum(e.amount) AS spent FROM events e
			WHERE e.user_id = u.id AND e.type = 'purchase') o ON true
		WHERE %s
		ORDER BY u.id
		LIMIT $%d OFFSET $%d`, filter, n+3, n+4),
		append(args, q.PerPage, (q.Page-1)*q.PerPage)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.FirstName, &m.LastName, &m.City, &m.Language,
			&m.SMSConsent, &m.EmailConsent, &m.OrderCount, &m.TotalSpent); err != nil {
			return page, err
		}
		page.Members = append(page.Members, m)
	}
	return page, rows.Err()
}
