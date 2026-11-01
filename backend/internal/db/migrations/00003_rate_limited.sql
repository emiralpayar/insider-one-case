-- +goose Up
-- How many times the provider answered 429 for a message. 429s do not count
-- as attempts (nothing was sent), so without this a provider that keeps
-- answering 429 would keep a message queued forever (DECISIONS #28).
alter table messages add column rate_limited int not null default 0;

-- +goose Down
alter table messages drop column rate_limited;
