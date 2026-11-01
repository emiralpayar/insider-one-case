// Package campaign holds campaign persistence, the approval step (audience →
// rendered messages in the queue) and the live status report.
package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/content"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/emiralpayar/campaign-composer/internal/verr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Campaign statuses.
const (
	StatusDraft         = "draft"
	StatusAudienceReady = "audience_ready"
	StatusContentReady  = "content_ready"
	StatusSending       = "sending"
	StatusCompleted     = "completed"
)

var (
	// ErrNotFound means no campaign has the given id.
	ErrNotFound = errors.New("campaign: not found")
	// ErrLocked means the campaign was approved and can no longer be edited.
	ErrLocked = errors.New("campaign: locked after approval")
	// ErrNotReady means the campaign is not in a state that allows the operation
	// (e.g. content before a rule, approval before content).
	ErrNotReady = errors.New("campaign: not ready")
)

// Campaign is a campaign row.
type Campaign struct {
	ID          uuid.UUID        `json:"id"`
	Name        string           `json:"name"`
	Brief       string           `json:"brief"`
	LLMProvider string           `json:"llm_provider"`
	Channels    []string         `json:"channels"`
	Status      string           `json:"status"`
	Rule        *segment.Rule    `json:"rule"`
	Content     *content.Content `json:"content"`
	ApprovedAt  *time.Time       `json:"approved_at"`
	CreatedAt   time.Time        `json:"created_at"`
}

// Store persists campaigns. LLMProvider is recorded on every created campaign.
type Store struct {
	pool        *pgxpool.Pool
	llmProvider string
}

// NewStore returns a Store; llmProvider is the active provider ("anthropic" | "mock").
func NewStore(pool *pgxpool.Pool, llmProvider string) *Store {
	return &Store{pool: pool, llmProvider: llmProvider}
}

const selectCols = `id, name, brief, llm_provider, channels, status, segment_rule, content, approved_at, created_at`

func scan(row pgx.Row) (Campaign, error) {
	var (
		c          Campaign
		rule, cont []byte
	)
	err := row.Scan(&c.ID, &c.Name, &c.Brief, &c.LLMProvider, &c.Channels, &c.Status, &rule, &cont, &c.ApprovedAt, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if rule != nil {
		c.Rule = new(segment.Rule)
		if err := json.Unmarshal(rule, c.Rule); err != nil {
			return c, fmt.Errorf("campaign %s: decode rule: %w", c.ID, err)
		}
	}
	if cont != nil {
		c.Content = new(content.Content)
		if err := json.Unmarshal(cont, c.Content); err != nil {
			return c, fmt.Errorf("campaign %s: decode content: %w", c.ID, err)
		}
	}
	return c, nil
}

// Create inserts a draft campaign with the default channels, named after the
// brief (DefaultName).
func (s *Store) Create(ctx context.Context, brief string) (Campaign, error) {
	return scan(s.pool.QueryRow(ctx,
		`insert into campaigns (id, name, brief, llm_provider, status) values ($1, $2, $3, $4, $5) returning `+selectCols,
		uuid.New(), DefaultName(brief), brief, s.llmProvider, StatusDraft))
}

// Get returns one campaign or ErrNotFound.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (Campaign, error) {
	return scan(s.pool.QueryRow(ctx, `select `+selectCols+` from campaigns where id=$1`, id))
}

// List returns the most recent campaigns, newest first.
func (s *Store) List(ctx context.Context, limit int) ([]Campaign, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `select `+selectCols+` from campaigns order by created_at desc, id limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Campaign{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveRule stores the (already validated) rule. A campaign without content
// becomes audience_ready; one with content stays content_ready (the content is
// re-validated against the new audience at approval).
func (s *Store) SaveRule(ctx context.Context, id uuid.UUID, r segment.Rule) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `update campaigns
		set segment_rule=$2,
		    status = case when content is null then 'audience_ready' else 'content_ready' end,
		    updated_at=now()
		where id=$1 and status in ('draft','audience_ready','content_ready')`, id, b)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return s.whyNot(ctx, id)
	}
	return nil
}

// SaveContent stores the (already validated) content; the campaign needs a
// rule first and becomes content_ready.
func (s *Store) SaveContent(ctx context.Context, id uuid.UUID, c content.Content) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `update campaigns
		set content=$2, status='content_ready', updated_at=now()
		where id=$1 and status in ('audience_ready','content_ready')`, id, b)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return s.whyNot(ctx, id)
	}
	return nil
}

// SaveContentChannels stores only the given channels' parts of c ("sms",
// "email"), merged into the stored content in one statement, and returns the
// content as stored. A regeneration of one channel therefore cannot
// overwrite an edit of the other channel saved while it was streaming.
func (s *Store) SaveContentChannels(ctx context.Context, id uuid.UUID, c content.Content, channels []string) (content.Content, error) {
	part := map[string]any{}
	for _, ch := range channels {
		switch ch {
		case "sms":
			part["sms"] = c.SMS
		case "email":
			part["email"] = c.Email
		}
	}
	b, err := json.Marshal(part)
	if err != nil {
		return content.Content{}, err
	}
	var stored []byte
	err = s.pool.QueryRow(ctx, `update campaigns
		set content = coalesce(content, '{"sms":"","email":{"subject":"","preheader":"","body":""}}'::jsonb) || $2::jsonb,
		    status='content_ready', updated_at=now()
		where id=$1 and status in ('audience_ready','content_ready')
		returning content`, id, b).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return content.Content{}, s.whyNot(ctx, id)
	}
	if err != nil {
		return content.Content{}, err
	}
	var out content.Content
	return out, json.Unmarshal(stored, &out)
}

// ValidateChannels checks a channel selection: non-empty subset of sms/email.
// It returns the de-duplicated list in canonical order.
func ValidateChannels(chs []string) ([]string, verr.List) {
	seen := map[string]bool{}
	var errs verr.List
	for i, ch := range chs {
		if ch != "sms" && ch != "email" {
			errs = append(errs, verr.Error{Path: fmt.Sprintf("channels[%d]", i), Code: "unknown_channel",
				Message: fmt.Sprintf("Bilinmeyen kanal: %q (izin verilenler: sms, email)", ch)})
			continue
		}
		seen[ch] = true
	}
	if len(errs) == 0 && len(seen) == 0 {
		errs = append(errs, verr.Error{Path: "channels", Code: "empty_channels", Message: "En az bir kanal seçilmeli"})
	}
	if len(errs) > 0 {
		return nil, errs
	}
	out := []string{}
	for _, ch := range []string{"sms", "email"} {
		if seen[ch] {
			out = append(out, ch)
		}
	}
	return out, nil
}

// SetChannels changes the enabled channels before approval.
func (s *Store) SetChannels(ctx context.Context, id uuid.UUID, chs []string) error {
	norm, errs := ValidateChannels(chs)
	if errs != nil {
		return errs
	}
	tag, err := s.pool.Exec(ctx, `update campaigns set channels=$2, updated_at=now()
		where id=$1 and status in ('draft','audience_ready','content_ready')`, id, norm)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return s.whyNot(ctx, id)
	}
	return nil
}

// whyNot explains a guarded update that touched no row.
func (s *Store) whyNot(ctx context.Context, id uuid.UUID) error {
	var status string
	err := s.pool.QueryRow(ctx, `select status from campaigns where id=$1`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if Locked(status) {
		return ErrLocked
	}
	return ErrNotReady
}

// Locked reports whether a campaign in this status can no longer be edited.
func Locked(status string) bool {
	return status != StatusDraft && status != StatusAudienceReady && status != StatusContentReady
}

// Querier is the subset of pgxpool.Pool / pgx.Tx used for reads.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// WorstCaseFor returns the longest first_name and city in the rule's
// audience. Empty fields (empty audience) are filled by content's defaults.
func WorstCaseFor(ctx context.Context, q Querier, r segment.Rule, asOf time.Time) (content.WorstCase, error) {
	where, args, err := segment.Compile(r, 0, asOf)
	if err != nil {
		return content.WorstCase{}, err
	}
	var wc content.WorstCase
	err = q.QueryRow(ctx, `select
		coalesce((select u.first_name from users u where `+where+` order by char_length(u.first_name) desc, u.id limit 1), ''),
		coalesce((select u.city from users u where `+where+` order by char_length(u.city) desc, u.id limit 1), '')`,
		args...).Scan(&wc.LongestFirstName, &wc.LongestCity)
	return wc, err
}
