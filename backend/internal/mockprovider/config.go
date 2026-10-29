// Package mockprovider is a fake SMS/email provider with idempotency and
// fault injection, used for local runs and exactly-once tests.
package mockprovider

import (
	"math/rand"
	"os"
	"strconv"
	"time"
)

// Config controls fault injection and behavior of the mock provider.
type Config struct {
	FailRate         float64 // 503, nothing stored
	RateLimited429   float64 // random 429, nothing stored
	LostResponseRate float64 // stored, then 502
	TimeoutRate      float64 // stored, wait TimeoutDelay, then 504

	LatencyMin, LatencyMax, TimeoutDelay time.Duration

	RatePerSecond int // per channel, trailing 1s window; <=0 disables
	Idempotency   bool

	Now  func() time.Time
	Rand *rand.Rand
}

// ConfigFromEnv builds a Config from MOCK_* environment variables.
func ConfigFromEnv() Config {
	return Config{
		FailRate:         envFloat("MOCK_FAIL_RATE", 0.05),
		RateLimited429:   envFloat("MOCK_429_RATE", 0.03),
		LostResponseRate: envFloat("MOCK_LOST_RESPONSE_RATE", 0.02),
		TimeoutRate:      envFloat("MOCK_TIMEOUT_RATE", 0.01),
		LatencyMin:       time.Duration(envInt("MOCK_LATENCY_MIN_MS", 10)) * time.Millisecond,
		LatencyMax:       time.Duration(envInt("MOCK_LATENCY_MAX_MS", 150)) * time.Millisecond,
		TimeoutDelay:     time.Duration(envInt("MOCK_TIMEOUT_DELAY_MS", 10000)) * time.Millisecond,
		RatePerSecond:    envInt("MOCK_RATE_PER_SECOND", 100),
		Idempotency:      envBool("MOCK_IDEMPOTENCY", true),
	}
}

func envFloat(k string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(k), 64); err == nil {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(k)); err == nil {
		return v
	}
	return def
}
