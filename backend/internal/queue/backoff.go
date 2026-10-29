package queue

import "time"

const (
	backoffBase = time.Second
	backoffCap  = 60 * time.Second
)

// Backoff returns the delay before retry number attempt (1-based) with
// "equal jitter": d = min(1s*2^(attempt-1), 60s), result = d/2 + d/2*rnd,
// where rnd is in [0,1].
func Backoff(attempt int, rnd float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := backoffCap
	if attempt <= 7 { // 2^6 s = 64s already exceeds the cap
		d = min(backoffBase<<(attempt-1), backoffCap)
	}
	half := d / 2
	return half + time.Duration(float64(half)*rnd)
}
