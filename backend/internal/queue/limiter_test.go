package queue

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// drain counts how many tokens can be taken at the current fake time.
func drain(l *Limiter) int {
	n := 0
	for {
		if _, ok := l.reserve(); !ok {
			return n
		}
		n++
	}
}

// passesOver counts tokens granted while stepping the clock through d in 1ms steps.
func passesOver(l *Limiter, c *fakeClock, d time.Duration) int {
	n := 0
	for s := time.Duration(0); s < d; s += time.Millisecond {
		n += drain(l)
		c.Advance(time.Millisecond)
	}
	return n
}

func TestLimiterAtMostMaxPerSecond(t *testing.T) {
	c := newFakeClock()
	l := NewLimiter(100, c.Now)
	if n := passesOver(l, c, time.Second); n > 100 || n < 85 {
		t.Fatalf("first second: %d grants", n)
	}
	// Any sliding 1s window over a longer run must hold at most 100.
	var times []time.Time
	for s := 0; s < 5000; s++ {
		for i := drain(l); i > 0; i-- {
			times = append(times, c.Now())
		}
		c.Advance(time.Millisecond)
	}
	if m := maxInWindow(times, time.Second); m > 100 {
		t.Fatalf("sliding window max %d > 100", m)
	}
	if len(times) < 400 {
		t.Fatalf("too slow: %d grants in 5s", len(times))
	}
}

func maxInWindow(ts []time.Time, w time.Duration) int {
	max, j := 0, 0
	for i := range ts {
		for ts[i].Sub(ts[j]) >= w {
			j++
		}
		if n := i - j + 1; n > max {
			max = n
		}
	}
	return max
}

func TestLimiterPauseAndRecovery(t *testing.T) {
	c := newFakeClock()
	l := NewLimiter(100, c.Now)
	if l.Rate() != 100 {
		t.Fatalf("initial rate %d", l.Rate())
	}
	l.Pause(2 * time.Second)
	if n := passesOver(l, c, 2*time.Second); n != 0 {
		t.Fatalf("granted %d during pause", n)
	}
	if r := l.Rate(); r != 70 {
		t.Fatalf("rate after pause %d want 70", r)
	}
	if n := passesOver(l, c, time.Second); n > 70 {
		t.Fatalf("granted %d in the first second after pause, want <=70", n)
	}
	if r := l.Rate(); r != 80 {
		t.Fatalf("rate 1s after pause %d want 80", r)
	}
	c.Advance(2 * time.Second)
	if r := l.Rate(); r != 100 {
		t.Fatalf("rate 3s after pause %d want 100", r)
	}
	c.Advance(10 * time.Second)
	if r := l.Rate(); r != 100 {
		t.Fatalf("rate capped: %d", r)
	}
}

func TestLimiterWaitHonorsContext(t *testing.T) {
	c := newFakeClock() // frozen: tokens never refill
	l := NewLimiter(100, c.Now)
	l.Pause(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := l.Wait(ctx); err == nil {
		t.Fatal("Wait returned nil while paused")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("Wait did not return promptly on ctx cancel")
	}
}

func TestLimiterWaitRealClock(t *testing.T) {
	l := NewLimiter(100, time.Now)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	for i := 0; i < 150; i++ {
		if err := l.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el < time.Second {
		t.Fatalf("150 waits in %v; limiter not limiting", el)
	}
}
