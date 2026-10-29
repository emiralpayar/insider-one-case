package queue

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CompleteCampaigns marks 'sending' campaigns whose messages are all terminal
// (sent/failed/unknown/skipped_no_consent) as 'completed' and returns how many
// it changed.
func CompleteCampaigns(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	tag, err := pool.Exec(ctx, `
update campaigns c set status='completed', updated_at=now()
where c.status='sending'
  and not exists (
    select 1 from messages m
    where m.campaign_id=c.id and m.status not in ('sent','failed','unknown','skipped_no_consent'))`)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
