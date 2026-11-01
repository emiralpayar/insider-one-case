-- +goose Up
-- Set once an attempt of the message had an uncertain outcome (5xx, timeout,
-- worker crash mid-send): the provider may already have delivered it, so a
-- later "certainly not sent" answer must not end it as failed or skipped
-- (DECISIONS #30).
alter table messages add column maybe_delivered bool not null default false;

-- +goose Down
alter table messages drop column maybe_delivered;
