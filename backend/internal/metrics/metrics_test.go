package metrics

import (
	"context"
	"errors"
	"strings"
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

func TestAPIMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	a := NewAPI(reg, func(ctx context.Context) ([]QueueDepthRow, error) {
		return []QueueDepthRow{{"sms", "queued", 7}}, nil
	})
	a.LLMCall("segment", LLMResultInvalid, 1, time.Second)
	a.LLMCall("segment", LLMResultOK, 2, time.Second)
	if v := testutil.ToFloat64(a.LLMCalls.WithLabelValues("segment", "ok")); v != 1 {
		t.Fatalf("calls %v", v)
	}
	if v := testutil.ToFloat64(a.LLMRepairs.WithLabelValues("segment")); v != 1 {
		t.Fatalf("repairs %v", v)
	}
	want := "# HELP composer_queue_depth Messages by channel and status, read from the database at scrape time.\n" +
		"# TYPE composer_queue_depth gauge\ncomposer_queue_depth{channel=\"sms\",status=\"queued\"} 7\n"
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "composer_queue_depth"); err != nil {
		t.Fatal(err)
	}
	var nilA *API
	nilA.LLMCall("segment", "ok", 1, 0) // must not panic

	reg2 := prometheus.NewRegistry()
	NewAPI(reg2, func(context.Context) ([]QueueDepthRow, error) { return nil, errors.New("db down") })
	if _, err := reg2.Gather(); err == nil {
		t.Fatal("expected gather error when depth read fails")
	}
}
