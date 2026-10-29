-- +goose Up
create table users (
  id bigserial primary key,
  first_name text not null default '',
  last_name text,
  email text unique not null,
  phone text not null,
  city text not null,
  language text not null check (language in ('tr','en')),
  sms_consent bool,
  email_consent bool,
  consent_updated_at timestamptz,
  created_at timestamptz not null default now()
);

create table events (
  id bigserial primary key,
  user_id bigint not null references users(id),
  type text not null check (type in ('cart_add','purchase')),
  occurred_at timestamptz not null,
  amount numeric
);
create index events_user_type_time_idx on events (user_id, type, occurred_at desc);

create table campaigns (
  id uuid primary key,
  brief text not null,
  llm_provider text not null,
  channels text[] not null default '{sms,email}',
  status text not null,
  segment_rule jsonb,
  content jsonb,
  approved_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table messages (
  id uuid primary key,
  campaign_id uuid not null references campaigns(id),
  user_id bigint not null references users(id),
  channel text not null check (channel in ('sms','email')),
  recipient text not null,
  subject text,
  body text not null,
  status text not null,
  attempts int not null default 0,
  next_attempt_at timestamptz not null default now(),
  locked_until timestamptz,
  last_error text,
  provider_message_id text,
  sent_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (campaign_id, user_id, channel)
);
create index messages_queue_idx on messages (channel, status, next_attempt_at);

create table llm_calls (
  id bigserial primary key,
  campaign_id uuid null,
  purpose text not null,
  attempt int not null default 1,
  request jsonb,
  response_text text,
  validation_errors jsonb,
  error text,
  model text,
  input_tokens int,
  output_tokens int,
  latency_ms int,
  created_at timestamptz not null default now()
);

-- +goose Down
drop table llm_calls;
drop table messages;
drop table campaigns;
drop table events;
drop table users;
