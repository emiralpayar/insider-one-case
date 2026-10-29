// Package queue implements the Postgres-backed message queue and the
// per-channel dispatcher (leader election, rate limiting, retries).
package queue

import (
	"context"
	"errors"
	"net"
	"syscall"
)

// Outcome is the classified result of one provider call.
type Outcome int

const (
	OutcomeSent        Outcome = iota // accepted by the provider
	OutcomeDuplicate                  // provider already had this idempotency key: sent earlier
	OutcomeRateLimited                // 429: certainly not sent, retry later without using an attempt
	OutcomePermanent                  // other 4xx: certainly not sent, do not retry
	OutcomeNotSent                    // request never reached the provider (dial/DNS failure)
	OutcomeUncertain                  // 5xx, timeout, reset after write: may or may not have been sent
)

func (o Outcome) String() string {
	switch o {
	case OutcomeSent:
		return "sent"
	case OutcomeDuplicate:
		return "duplicate"
	case OutcomeRateLimited:
		return "rate_limited"
	case OutcomePermanent:
		return "permanent"
	case OutcomeNotSent:
		return "not_sent"
	case OutcomeUncertain:
		return "uncertain"
	}
	return "invalid"
}

// Classify maps an HTTP status or transport error to an Outcome.
//
// A 200 is reported as Sent here; the HTTP provider inspects the body and
// turns a 200 {"duplicate":true} into OutcomeDuplicate itself.
//
// Only errors that prove the request never reached the provider are NotSent
// (connection refused, DNS failure, any dial error). Everything else —
// timeouts, resets/EOF after the request was written, unknown errors — is
// Uncertain: the provider may have accepted the message.
func Classify(status int, err error) Outcome {
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return OutcomeNotSent
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return OutcomeNotSent
		}
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return OutcomeNotSent
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return OutcomeUncertain
		}
		return OutcomeUncertain
	}
	switch {
	case status >= 200 && status < 300:
		return OutcomeSent
	case status == 429:
		return OutcomeRateLimited
	case status >= 400 && status < 500:
		return OutcomePermanent
	default: // 5xx and anything unexpected
		return OutcomeUncertain
	}
}
