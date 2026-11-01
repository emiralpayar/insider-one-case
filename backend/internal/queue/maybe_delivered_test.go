package queue

import (
	"context"
	"sync"
	"testing"
	"time"
)

// seqProvider answers each call with the next outcome of a fixed sequence
// (the last one repeats). It claims idempotency support.
type seqProvider struct {
	mu   sync.Mutex
	outs []Outcome
	n    int
	hook func(n int) // called before answering call n (1-based)
}

func (p *seqProvider) SupportsIdempotency() bool { return true }

func (p *seqProvider) Send(_ context.Context, _ OutboundMessage) SendResult {
	p.mu.Lock()
	p.n++
	n := p.n
	o := p.outs[min(n, len(p.outs))-1]
	p.mu.Unlock()
	if p.hook != nil {
		p.hook(n)
	}
	return SendResult{Outcome: o, Detail: o.String()}
}

// An uncertain attempt may have delivered the message, so a later "certainly
// not sent" answer must end it as unknown, never failed or skipped.
func TestMaybeDeliveredNeverBecomesFailedOrSkipped(t *testing.T) {
	cases := []struct {
		name string
		outs []Outcome
		// revoke consent after the first (uncertain) attempt
		revoke bool
	}{
		{"not sent until exhausted", []Outcome{OutcomeUncertain, OutcomeNotSent}, false},
		{"permanent 4xx", []Outcome{OutcomeUncertain, OutcomePermanent}, false},
		{"consent revoked", []Outcome{OutcomeUncertain}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := newDB(t, 50)
			camp := insertCampaign(t, pool, "sending")
			ids := insertMessages(t, pool, camp, "sms", 1)
			mustExec(t, pool, `update users set sms_consent=true`)
			p := &seqProvider{outs: tc.outs}
			if tc.revoke {
				p.hook = func(n int) {
					if n == 1 {
						mustExec(t, pool, `update users set sms_consent=false`)
					}
				}
			}
			d := newDispatcher(pool, p, "sms")
			d.MaxAttempts = 3
			stop := runAll(t, d)
			waitFor(t, 30*time.Second, "terminal", allTerminal(t, pool, camp))
			stop()
			if r := getMessage(t, pool, ids[0]); r.Status != "unknown" {
				t.Fatalf("status = %s (last_error %v), want unknown", r.Status, r.LastErr)
			}
		})
	}
	// Control: without an uncertain attempt the same answers mean failed.
	pool := newDB(t, 50)
	camp := insertCampaign(t, pool, "sending")
	ids := insertMessages(t, pool, camp, "sms", 1)
	mustExec(t, pool, `update users set sms_consent=true`)
	stop := runAll(t, newDispatcher(pool, &seqProvider{outs: []Outcome{OutcomePermanent}}, "sms"))
	waitFor(t, 30*time.Second, "terminal", allTerminal(t, pool, camp))
	stop()
	if r := getMessage(t, pool, ids[0]); r.Status != "failed" {
		t.Fatalf("control: status = %s", r.Status)
	}
}
