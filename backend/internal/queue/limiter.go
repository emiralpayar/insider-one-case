package queue

import (
	"context"
	"sync"
	"time"
)

const (
	// limiterSafety stretches the pacing interval so that `max` grants span
	// 1s+limiterSafety instead of 1s. The provider counts requests by their
	// arrival time; scheduling/network jitter between our grant and its
	// arrival could otherwise squeeze max+1 requests into one provider-side
	// 1s window. Costs ~5% throughput (≈95/s at max=100).
	limiterSafety = 50 * time.Millisecond
	// limiterRecoveryStep is added to the rate for every full second without a Pause.
	limiterRecoveryStep = 10
	// limiterMaxSleep bounds a single sleep inside Wait so that a Pause or a
	// rate change is noticed promptly (and a frozen fake clock cannot hang Wait).
	limiterMaxSleep = 100 * time.Millisecond
)

// Limiter paces sends on one channel: at most Rate() grants per second,
// evenly spaced (no bursts). Pause(d) blocks grants for d and drops the rate
// to 70% of max; afterwards the rate grows by 10/s per full second until it
// is back at max. It is used only by the channel leader, so the limit is
// counted in one place.
type Limiter struct {
	mu          sync.Mutex
	max         int
	now         func() time.Time
	next        time.Time // earliest time of the next grant
	pausedUntil time.Time // zero: never paused
}

// NewLimiter returns a limiter allowing max grants per second. now is the clock
// (time.Now in production, a fake clock in tests).
func NewLimiter(max int, now func() time.Time) *Limiter {
	if max < 1 {
		max = 1
	}
	if now == nil {
		now = time.Now
	}
	return &Limiter{max: max, now: now}
}

// Rate returns the current allowed rate in messages per second.
func (l *Limiter) Rate() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rateAt(l.now())
}

func (l *Limiter) rateAt(now time.Time) int {
	if l.pausedUntil.IsZero() {
		return l.max
	}
	reduced := max(l.max*7/10, 1)
	if now.Before(l.pausedUntil) {
		return reduced
	}
	steps := int(now.Sub(l.pausedUntil) / time.Second)
	return min(l.max, reduced+limiterRecoveryStep*steps)
}

// Pause stops all grants for d (e.g. the provider's Retry-After) and resets
// the rate to 70% of max; recovery starts when the pause ends.
func (l *Limiter) Pause(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until := l.now().Add(d); until.After(l.pausedUntil) {
		l.pausedUntil = until
	}
}

// reserve takes a token if one is available now; otherwise it reports how
// long until the next one could be.
func (l *Limiter) reserve() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	at := now
	if l.next.After(at) {
		at = l.next
	}
	if l.pausedUntil.After(at) {
		at = l.pausedUntil
	}
	if at.After(now) {
		return at.Sub(now), false
	}
	interval := (time.Second + limiterSafety) / time.Duration(l.rateAt(now))
	l.next = now.Add(interval)
	return 0, true
}

// Wait blocks until a send is allowed or ctx is done. It sleeps on timers
// (never spins) and never consumes a token once ctx is done.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		d, ok := l.reserve()
		if ok {
			return nil
		}
		t := time.NewTimer(min(d, limiterMaxSleep))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}
