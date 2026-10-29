package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/verr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CallRecord is one model call (one attempt) as stored in llm_calls.
type CallRecord struct {
	CampaignID                *uuid.UUID
	Purpose                   string
	Attempt                   int
	Request                   any
	ResponseText              string
	ValidationErrors          verr.List
	Err                       string
	Model                     string
	InputTokens, OutputTokens int
	Latency                   time.Duration
}

// CallLogger persists call records.
type CallLogger interface {
	Log(ctx context.Context, r CallRecord) error
}

type pgCallLogger struct{ pool *pgxpool.Pool }

// NewPGCallLogger returns a CallLogger writing to the llm_calls table.
func NewPGCallLogger(pool *pgxpool.Pool) CallLogger { return &pgCallLogger{pool: pool} }

func (l *pgCallLogger) Log(ctx context.Context, r CallRecord) error {
	var reqJSON, veJSON []byte
	var err error
	if r.Request != nil {
		if reqJSON, err = json.Marshal(r.Request); err != nil {
			return fmt.Errorf("llm: marshal request: %w", err)
		}
	}
	if len(r.ValidationErrors) > 0 {
		if veJSON, err = json.Marshal(r.ValidationErrors); err != nil {
			return fmt.Errorf("llm: marshal validation errors: %w", err)
		}
	}
	_, err = l.pool.Exec(ctx, `insert into llm_calls
		(campaign_id, purpose, attempt, request, response_text, validation_errors, error, model,
		 input_tokens, output_tokens, latency_ms)
		values ($1, $2, $3, $4, $5, $6, nullif($7, ''), nullif($8, ''), $9, $10, $11)`,
		r.CampaignID, r.Purpose, r.Attempt, nullJSON(reqJSON), r.ResponseText, nullJSON(veJSON),
		r.Err, r.Model, r.InputTokens, r.OutputTokens, r.Latency.Milliseconds())
	if err != nil {
		return fmt.Errorf("llm: insert llm_calls: %w", err)
	}
	return nil
}

// nullJSON maps an empty document to SQL NULL; otherwise passes the JSON text
// so that pgx sends it as jsonb rather than bytea.
func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}
