-- +goose Up
-- A human-readable campaign name. New campaigns get campaign.DefaultName(brief);
-- existing rows are backfilled with the same rule: whitespace collapsed, at
-- most 60 characters cut at a word boundary, "…" appended when cut.
alter table campaigns add column name text not null default '';

with n as (
  select id, btrim(regexp_replace(brief, '\s+', ' ', 'g')) as b from campaigns
), c as (
  select id, b, regexp_replace(left(b, 61), ' [^ ]*$', '') as cut from n
)
update campaigns set name = case
    when char_length(c.b) <= 60 then c.b
    else rtrim(case when char_length(c.cut) > 60 then left(c.b, 60) else c.cut end, ' ,.;:-–') || '…'
  end
from c
where campaigns.id = c.id;

-- +goose Down
alter table campaigns drop column name;
