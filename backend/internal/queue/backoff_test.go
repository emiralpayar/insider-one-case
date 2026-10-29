package queue

import (
	"testing"
	"time"
)

func TestBackoff(t *testing.T) {
	cases := []struct {
		attempt int
		rnd     float64
		want    time.Duration
	}{
		{1, 1, time.Second},
		{2, 1, 2 * time.Second},
		{3, 1, 4 * time.Second},
		{10, 1, 60 * time.Second},
		{100, 1, 60 * time.Second},
		{1, 0, 500 * time.Millisecond},
		{3, 0, 2 * time.Second},
		{10, 0, 30 * time.Second},
		{3, 0.5, 3 * time.Second},
	}
	for _, c := range cases {
		if got := Backoff(c.attempt, c.rnd); got != c.want {
			t.Errorf("Backoff(%d,%v)=%v want %v", c.attempt, c.rnd, got, c.want)
		}
	}
}
