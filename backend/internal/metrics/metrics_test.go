package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestWorkerMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	w := NewWorker(reg)
	w.Outcome("sms", "sent")
	w.Outcome("sms", "sent")
	w.RateLimitedInc("sms")
	w.SetSendRate("sms", 70)
	w.ObserveLatency("sms", 10*time.Millisecond)
	if v := testutil.ToFloat64(w.Messages.WithLabelValues("sms", "sent")); v != 2 {
		t.Fatalf("messages %v", v)
	}
	if v := testutil.ToFloat64(w.SendRate.WithLabelValues("sms")); v != 70 {
		t.Fatalf("rate %v", v)
	}
	n, err := testutil.GatherAndCount(reg, "composer_provider_latency_seconds", "composer_rate_limited_total")
	if err != nil || n != 2 {
		t.Fatalf("count %d err %v", n, err)
	}
	var nilW *Worker
	nilW.Outcome("sms", "sent") // must not panic
}
