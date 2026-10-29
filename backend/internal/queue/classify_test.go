package queue

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestClassify(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}
	cases := []struct {
		name   string
		status int
		err    error
		want   Outcome
	}{
		{"202 accepted", 202, nil, OutcomeSent},
		{"200 (duplicate decided by provider body)", 200, nil, OutcomeSent},
		{"429", 429, nil, OutcomeRateLimited},
		{"400", 400, nil, OutcomePermanent},
		{"404", 404, nil, OutcomePermanent},
		{"503", 503, nil, OutcomeUncertain},
		{"502 lost response", 502, nil, OutcomeUncertain},
		{"504", 504, nil, OutcomeUncertain},
		{"ECONNREFUSED dial", 0, fmt.Errorf("post: %w", refused), OutcomeNotSent},
		{"bare ECONNREFUSED", 0, syscall.ECONNREFUSED, OutcomeNotSent},
		{"dns", 0, &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "x"}}, OutcomeNotSent},
		{"deadline", 0, context.DeadlineExceeded, OutcomeUncertain},
		{"wrapped deadline", 0, fmt.Errorf("x: %w", context.DeadlineExceeded), OutcomeUncertain},
		{"conn reset on read", 0, &net.OpError{Op: "read", Err: syscall.ECONNRESET}, OutcomeUncertain},
		{"other", 0, errors.New("EOF"), OutcomeUncertain},
	}
	for _, c := range cases {
		if got := Classify(c.status, c.err); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
