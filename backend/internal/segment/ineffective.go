package segment

import (
	"context"
	"fmt"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/verr"

	"github.com/jackc/pgx/v5/pgxpool"
)

// IneffectiveConditions returns the indexes of r's conditions that every user
// satisfies on its own ("order_count lte 1000" when nobody has 1000 orders).
// Such a condition does not narrow the audience; for model output it is a
// placeholder that hides an unmapped or misread audience (DECISIONS #26).
// A rule with fewer than one condition, or an invalid rule, has none.
func IneffectiveConditions(ctx context.Context, pool *pgxpool.Pool, r Rule, asOf time.Time) ([]int, error) {
	if r.Audience != "filtered" {
		return nil, nil
	}
	if _, errs := Validate(r); len(errs) > 0 {
		return nil, nil
	}
	var out []int
	for i, c := range r.Conditions {
		where, args, err := Compile(Rule{Audience: "filtered", Conditions: []Condition{c}}, 0, asOf)
		if err != nil {
			return nil, err
		}
		var excluded bool
		// coalesce: a NULL predicate excludes the user, as in a WHERE clause.
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users u WHERE NOT coalesce((`+where+`), false))`, args...).Scan(&excluded); err != nil {
			return nil, err
		}
		if !excluded {
			out = append(out, i)
		}
	}
	return out, nil
}

// IneffectiveCheck adapts IneffectiveConditions to llm.Composer.RuleCheck:
// each condition every user satisfies becomes a tautological_condition error
// that the model repairs by dropping the condition (which, by definition,
// does not change the audience).
func IneffectiveCheck(pool *pgxpool.Pool) func(context.Context, Rule) verr.List {
	return func(ctx context.Context, r Rule) verr.List {
		idx, err := IneffectiveConditions(ctx, pool, r, time.Now())
		if err != nil {
			return nil // a DB hiccup must not fail generation; Preview reports it again
		}
		var errs verr.List
		for _, i := range idx {
			errs = append(errs, verr.Error{Path: fmt.Sprintf("conditions[%d]", i), Code: "tautological_condition",
				Message: fmt.Sprintf("%s matches every user in the data, so it does not narrow the audience; remove it (if the audience cannot be mapped, put the phrase in unmapped)", describe(r.Conditions[i]))})
		}
		return errs
	}
}
