package segment

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SampleUser is a small example of a matched user.
type SampleUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	City      string `json:"city"`
}

// Warning is a non-fatal audience notice.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PreviewResult summarizes the audience of a rule.
type PreviewResult struct {
	Total           int          `json:"total"`
	SMSEligible     int          `json:"sms_eligible"`
	EmailEligible   int          `json:"email_eligible"`
	AllUsers        int          `json:"all_users"`
	EnglishSpeakers int          `json:"english_speakers"`
	Sample          []SampleUser `json:"sample"`
	Warnings        []Warning    `json:"warnings"`
}

// MatchIDs returns the ids of all matching users, ordered by id.
func MatchIDs(ctx context.Context, pool *pgxpool.Pool, r Rule, asOf time.Time) ([]int64, error) {
	where, args, err := Compile(r, 0, asOf)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT u.id FROM users u WHERE `+where+` ORDER BY u.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Preview counts the audience, samples users and derives warnings.
func Preview(ctx context.Context, pool *pgxpool.Pool, r Rule, asOf time.Time) (PreviewResult, error) {
	where, args, err := Compile(r, 0, asOf)
	if err != nil {
		return PreviewResult{}, err
	}
	var p PreviewResult
	err = pool.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE u.sms_consent IS TRUE),
		count(*) FILTER (WHERE u.email_consent IS TRUE),
		count(*) FILTER (WHERE u.language='en')
		FROM users u WHERE `+where, args...).Scan(&p.Total, &p.SMSEligible, &p.EmailEligible, &p.EnglishSpeakers)
	if err != nil {
		return p, err
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&p.AllUsers); err != nil {
		return p, err
	}
	rows, err := pool.Query(ctx, `SELECT u.id, u.first_name, u.city FROM users u WHERE `+where+` ORDER BY u.id LIMIT 5`, args...)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	p.Sample = []SampleUser{}
	for rows.Next() {
		var s SampleUser
		if err := rows.Scan(&s.ID, &s.FirstName, &s.City); err != nil {
			return p, err
		}
		p.Sample = append(p.Sample, s)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	p.Warnings = SizeWarnings(p.Total, p.AllUsers)
	if len(r.Unmapped) > 0 {
		p.Warnings = append(p.Warnings, Warning{"unmapped", fmt.Sprintf("Parts of the brief could not be mapped: %v", r.Unmapped)})
	}
	ineff, err := IneffectiveConditions(ctx, pool, r, asOf)
	if err != nil {
		return p, err
	}
	for _, i := range ineff {
		p.Warnings = append(p.Warnings, Warning{"ineffective_condition",
			fmt.Sprintf("%d. koşul (%s) tüm kullanıcıları kapsıyor; kitleyi daraltmıyor.", i+1, describe(r.Conditions[i]))})
	}
	if p.EnglishSpeakers > 0 {
		p.Warnings = append(p.Warnings, Warning{"mixed_language", fmt.Sprintf("%d English-speaking users are in the audience.", p.EnglishSpeakers)})
	}
	return p, nil
}

// SizeWarnings returns the audience-size warnings (empty_audience,
// large_audience: more than 50% of all users). It is shared by Preview and the
// approval step so that the threshold lives in one place.
func SizeWarnings(total, allUsers int) []Warning {
	w := []Warning{}
	if total == 0 {
		w = append(w, Warning{"empty_audience", "No users match this segment."})
	}
	if total*2 > allUsers {
		w = append(w, Warning{"large_audience", fmt.Sprintf("Audience is %d of %d users (more than 50%%).", total, allUsers)})
	}
	return w
}

// describe renders a condition compactly for warnings ("order_count lte 1000").
func describe(c Condition) string { return c.Field + " " + c.Op + " " + string(c.Value) }
