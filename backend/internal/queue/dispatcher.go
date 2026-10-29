package queue

import (
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/metrics"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	leaseDuration   = 30 * time.Second
	leaderRetry     = time.Second            // non-leaders retry the advisory lock this often
	leaderHealth    = time.Second            // leader pings its lock connection this often
	idlePoll        = 200 * time.Millisecond // claim poll interval when the queue is empty
	dbOpTimeout     = 10 * time.Second
	jobTimeout      = 30 * time.Second // DB updates of one job; outlives token wait + provider timeout
	defaultRetryAft = time.Second
	maxRetryAfter   = 10 * time.Second // clamp for provider Retry-After (pause and reschedule)
	minLeaseLeft    = 5 * time.Second  // do not start a send with less lease than this left
)

// Dispatcher sends one channel's queued messages. Any number of Dispatchers
// may run for the same channel (in one or many processes); exactly one of
// them — the holder of the channel's advisory lock — claims and sends, so the
// channel's rate limit is enforced in a single place (DECISIONS #2).
//
// Zero-valued fields get defaults when Run starts. Run must not be called
// concurrently on the same Dispatcher value.
type Dispatcher struct {
	Pool         *pgxpool.Pool
	Store        *Store
	Provider     Provider
	Channel      string
	Limiter      *Limiter
	Concurrency  int            // parallel provider calls (default 20)
	BatchSize    int            // max rows claimed per query (default 100)
	MaxAttempts  int            // default 5
	Rand         func() float64 // jitter source in [0,1) (default math/rand/v2)
	BackoffScale time.Duration  // real delay = Backoff()*BackoffScale/1s (default 1s; tests use 1ms)
	Metrics      *metrics.Worker
}

func (d *Dispatcher) applyDefaults() {
	if d.Store == nil {
		d.Store = NewStore(d.Pool)
	}
	if d.Limiter == nil {
		d.Limiter = NewLimiter(100, time.Now)
	}
	if d.Concurrency <= 0 {
		d.Concurrency = 20
	}
	if d.BatchSize <= 0 {
		d.BatchSize = 100
	}
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = 5
	}
	if d.Rand == nil {
		d.Rand = rand.Float64
	}
	if d.BackoffScale <= 0 {
		d.BackoffScale = time.Second
	}
}

// Run competes for the channel's leadership until ctx is done. While leader
// it claims and sends; it returns after in-flight sends have finished and the
// lock has been released.
func (d *Dispatcher) Run(ctx context.Context) error {
	d.applyDefaults()
	for ctx.Err() == nil {
		conn, err := d.tryLock(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("dispatch %s: leader lock: %v", d.Channel, err)
		}
		if conn != nil {
			d.lead(ctx, conn)
			d.unlock(conn)
			continue
		}
		sleepCtx(ctx, leaderRetry)
	}
	return nil
}

// tryLock takes the channel's session-level advisory lock on a dedicated
// connection. It returns nil (and no error) when another worker is leader.
func (d *Dispatcher) tryLock(ctx context.Context) (*pgxpool.Conn, error) {
	conn, err := d.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var got bool
	if err := conn.QueryRow(ctx, `select pg_try_advisory_lock(hashtext('dispatch:'||$1))`, d.Channel).Scan(&got); err != nil {
		discard(conn) // we may hold the lock without knowing; ending the session drops it
		return nil, err
	}
	if !got {
		conn.Release()
		return nil, nil
	}
	return conn, nil
}

// unlock releases the advisory lock and returns conn to the pool. If the
// unlock cannot be confirmed the connection is closed instead, which ends the
// session and drops the lock — a pooled connection must never keep it.
func (d *Dispatcher) unlock(conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ok bool
	if err := conn.QueryRow(ctx, `select pg_advisory_unlock(hashtext('dispatch:'||$1))`, d.Channel).Scan(&ok); err != nil || !ok {
		discard(conn)
		return
	}
	conn.Release()
}

func discard(conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = conn.Conn().Close(ctx)
	conn.Release()
}

// lead runs the claim → send loop while this process holds the lock. It
// returns when ctx is done or the lock connection fails a ping (leadership
// lost); in both cases it stops claiming, lets in-flight sends finish, and
// hands claimed-but-unsent jobs back to the queue.
func (d *Dispatcher) lead(ctx context.Context, conn *pgxpool.Conn) {
	leadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Never hold more claimed jobs than the worker pool can take at once:
	// every claimed job is picked up by a worker immediately, so nothing
	// sits in an in-process buffer while its lease runs down.
	jobs := make(chan Job, d.Concurrency)
	var inflight atomic.Int64 // claimed and not yet finished; <= Concurrency
	finished := make(chan struct{}, 1)
	var wg sync.WaitGroup
	for range d.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				d.process(leadCtx, j)
				inflight.Add(-1)
				select {
				case finished <- struct{}{}:
				default:
				}
			}
		}()
	}

	for leadCtx.Err() == nil {
		rate := d.Limiter.Rate()
		d.Metrics.SetSendRate(d.Channel, rate)
		// At most ~1s worth of tokens and at most the worker pool size.
		capacity := max(min(d.BatchSize, rate, d.Concurrency), 1)
		n := int(inflight.Load())
		// Refill once half the pool is free, so claims come in batches
		// rather than one query per finished send.
		if n > capacity/2 {
			waitAny(leadCtx, finished, idlePoll)
			continue
		}
		// The lock lives exactly as long as conn's session. Confirm it is
		// still alive before taking more work; if not, another worker may
		// already be leader, so stop sending.
		if !ping(conn) {
			log.Printf("dispatch %s: lost leadership (lock connection failed)", d.Channel)
			break
		}
		// Claim on a non-cancellable context: a claim interrupted after
		// commit would leave leased rows nobody knows about.
		cctx, ccancel := context.WithTimeout(context.WithoutCancel(leadCtx), dbOpTimeout)
		claimed, err := d.Store.Claim(cctx, d.Channel, capacity-n, leaseDuration)
		ccancel()
		if err != nil {
			log.Printf("dispatch %s: claim: %v", d.Channel, err)
			sleepCtx(leadCtx, idlePoll)
			continue
		}
		inflight.Add(int64(len(claimed)))
		for _, j := range claimed {
			jobs <- j // never blocks: len(jobs) <= inflight <= Concurrency
		}
		if len(claimed) == 0 {
			sleepCtx(leadCtx, idlePoll)
		}
	}
	cancel() // also on leadership loss: workers release jobs not yet sent
	close(jobs)
	wg.Wait()
}

func ping(conn *pgxpool.Conn) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := conn.Exec(ctx, "select 1")
	return err == nil
}

// process applies the decision table (spec §10.4) to one claimed job.
// Provider calls and the resulting status updates use a context detached
// from shutdown: a send that has started is always allowed to finish and be
// recorded (the HTTP client timeout bounds it). Every status update is
// fenced by the job's lease token (see Store), so a stale copy is a no-op.
func (d *Dispatcher) process(leadCtx context.Context, j Job) {
	bg := context.WithoutCancel(leadCtx)
	ctx, cancel := context.WithTimeout(bg, jobTimeout)
	defer cancel()
	mark := func(outcome string, err error) {
		switch {
		case errors.Is(err, ErrLeaseLost):
			log.Printf("dispatch %s: %s: lease lost before recording %s (row was reclaimed)", d.Channel, j.ID, outcome)
		case err != nil:
			// The row stays 'sending' and is recovered after its lease expires.
			log.Printf("dispatch %s: mark %s %s: %v", d.Channel, j.ID, outcome, err)
		}
		d.Metrics.Outcome(d.Channel, outcome)
	}
	release := func(why string) {
		if err := d.Store.MarkRetry(ctx, j, time.Now(), true, why); err != nil && !errors.Is(err, ErrLeaseLost) {
			log.Printf("dispatch %s: release %s: %v", d.Channel, j.ID, err)
		}
	}
	idem := d.Provider.SupportsIdempotency()

	// Recovery is checked first: a recovered row may already have been
	// delivered, so it must not be reported as skipped_no_consent.
	if j.Recovered && !idem {
		// A previous worker died mid-request: it may have been delivered and
		// the provider cannot deduplicate a resend. At-most-once.
		mark("unknown", d.Store.MarkUnknown(ctx, j, "worker crashed mid-send"))
		return
	}
	if j.Recovered && j.Attempts > d.MaxAttempts {
		mark("unknown", d.Store.MarkUnknown(ctx, j, "worker crashed mid-send; attempts exhausted"))
		return
	}
	// HasConsent was read from users in the same statement that claimed the
	// job, i.e. immediately before sending (plus at most the token wait).
	if !j.HasConsent {
		mark("skipped_no_consent", d.Store.MarkSkipped(ctx, j))
		return
	}

	if err := d.Limiter.Wait(leadCtx); err != nil {
		// Stopping before the request was made: certainly not sent, give it back.
		release("dispatcher stopped before send")
		return
	}
	// Do not start a send whose lease could expire before it is recorded:
	// another worker could reclaim the row while our request is in flight.
	if time.Until(j.LockedUntil) < minLeaseLeft {
		release("lease nearly expired before send")
		return
	}

	start := time.Now()
	res := d.Provider.Send(bg, OutboundMessage{ID: j.ID, Channel: j.Channel, To: j.Recipient, Subject: j.Subject, Body: j.Body})
	d.Metrics.ObserveLatency(d.Channel, time.Since(start))

	now := time.Now()
	switch res.Outcome {
	case OutcomeSent, OutcomeDuplicate:
		mark(res.Outcome.String(), d.Store.MarkSent(ctx, j, res.ProviderID))
	case OutcomeRateLimited:
		ra := res.RetryAfter
		if ra <= 0 {
			ra = defaultRetryAft
		}
		// Clamp: a huge Retry-After must not stall the channel past the
		// leases of jobs already handed to workers.
		ra = min(ra, maxRetryAfter)
		d.Limiter.Pause(ra)
		d.Metrics.RateLimitedInc(d.Channel)
		mark("rate_limited", d.Store.MarkRetry(ctx, j, now.Add(ra), true, res.Detail))
	case OutcomePermanent:
		mark("failed", d.Store.MarkFailed(ctx, j, res.Detail))
	case OutcomeNotSent:
		if j.Attempts >= d.MaxAttempts {
			mark("failed", d.Store.MarkFailed(ctx, j, res.Detail))
		} else {
			mark("retry", d.Store.MarkRetry(ctx, j, now.Add(d.backoff(j.Attempts)), false, res.Detail))
		}
	default: // OutcomeUncertain
		switch {
		case !idem:
			mark("unknown", d.Store.MarkUnknown(ctx, j, res.Detail))
		case j.Attempts >= d.MaxAttempts:
			mark("unknown", d.Store.MarkUnknown(ctx, j, res.Detail))
		default:
			// Safe: the resend carries the same Idempotency-Key.
			mark("retry", d.Store.MarkRetry(ctx, j, now.Add(d.backoff(j.Attempts)), false, res.Detail))
		}
	}
}

func (d *Dispatcher) backoff(attempt int) time.Duration {
	return time.Duration(float64(Backoff(attempt, d.Rand())) * float64(d.BackoffScale) / float64(time.Second))
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func waitAny(ctx context.Context, ch <-chan struct{}, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-ch:
	case <-t.C:
	}
}
