package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OutboundMessage is one message handed to a provider. ID is the idempotency key.
type OutboundMessage struct {
	ID      uuid.UUID
	Channel string
	To      string
	Subject string
	Body    string
}

// SendResult is the classified outcome of a send.
type SendResult struct {
	Outcome    Outcome
	RetryAfter time.Duration // set for OutcomeRateLimited when the provider sent Retry-After
	ProviderID string
	Detail     string // human-readable error detail for last_error
}

// Provider sends messages. SupportsIdempotency reports whether the provider
// deduplicates on the Idempotency-Key header, which makes resending after an
// uncertain outcome safe.
type Provider interface {
	Send(ctx context.Context, m OutboundMessage) SendResult
	SupportsIdempotency() bool
}

type httpProvider struct {
	base   string
	idem   bool
	client *http.Client
}

// NewHTTPProvider returns a Provider speaking the mock provider HTTP contract
// (POST /v1/sms, POST /v1/email with an Idempotency-Key header).
func NewHTTPProvider(baseURL string, supportsIdempotency bool, timeout time.Duration) Provider {
	return &httpProvider{
		base:   strings.TrimRight(baseURL, "/"),
		idem:   supportsIdempotency,
		client: &http.Client{Timeout: timeout},
	}
}

func (p *httpProvider) SupportsIdempotency() bool { return p.idem }

type sendBody struct {
	To      string `json:"to"`
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

type sendResp struct {
	ID        string `json:"id"`
	Duplicate bool   `json:"duplicate"`
	Error     string `json:"error"`
}

func (p *httpProvider) Send(ctx context.Context, m OutboundMessage) SendResult {
	payload, err := json.Marshal(sendBody{To: m.To, Subject: m.Subject, Body: m.Body})
	if err != nil {
		return SendResult{Outcome: OutcomePermanent, Detail: "encode: " + err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/v1/"+m.Channel, bytes.NewReader(payload))
	if err != nil {
		return SendResult{Outcome: OutcomePermanent, Detail: "request: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", m.ID.String())
	if !p.idem {
		// net/http treats a request with an Idempotency-Key header and a
		// GetBody as replayable and may silently resend it on a reused
		// keep-alive connection that dies after the request was written.
		// Without provider-side dedup that would be a hidden double send,
		// so make the body non-rewindable: the transport then returns the
		// error to us (classified Uncertain → unknown) instead of retrying.
		req.GetBody = nil
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return SendResult{Outcome: Classify(0, err), Detail: err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var body sendResp
	_ = json.Unmarshal(raw, &body)

	res := SendResult{Outcome: Classify(resp.StatusCode, nil), ProviderID: body.ID}
	switch res.Outcome {
	case OutcomeSent:
		if resp.StatusCode == http.StatusOK && body.Duplicate {
			res.Outcome = OutcomeDuplicate
		}
	case OutcomeRateLimited:
		if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && s > 0 {
			res.RetryAfter = time.Duration(s) * time.Second
		}
		res.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
	default:
		res.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		if body.Error != "" {
			res.Detail += ": " + body.Error
		}
	}
	return res
}
