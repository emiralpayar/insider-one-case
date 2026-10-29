// Package metrics holds the Prometheus instruments exposed by the worker and the API.
package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Worker groups the dispatcher metrics. A nil *Worker is valid and records nothing.
type Worker struct {
	Messages    *prometheus.CounterVec   // composer_messages_total{channel,outcome}
	Latency     *prometheus.HistogramVec // composer_provider_latency_seconds{channel}
	RateLimited *prometheus.CounterVec   // composer_rate_limited_total{channel}
	SendRate    *prometheus.GaugeVec     // composer_send_rate{channel}
}

// NewWorker creates the worker metrics and registers them on reg.
func NewWorker(reg prometheus.Registerer) *Worker {
	w := &Worker{
		Messages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "composer_messages_total",
			Help: "Messages processed by the dispatcher, by channel and outcome.",
		}, []string{"channel", "outcome"}),
		Latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "composer_provider_latency_seconds",
			Help:    "Provider send call latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"channel"}),
		RateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "composer_rate_limited_total",
			Help: "429 responses received from the provider.",
		}, []string{"channel"}),
		SendRate: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "composer_send_rate",
			Help: "Current allowed send rate (messages/second) of the channel limiter.",
		}, []string{"channel"}),
	}
	reg.MustRegister(w.Messages, w.Latency, w.RateLimited, w.SendRate)
	return w
}

// Outcome counts one processed message.
func (w *Worker) Outcome(channel, outcome string) {
	if w != nil {
		w.Messages.WithLabelValues(channel, outcome).Inc()
	}
}

// ObserveLatency records a provider call duration.
func (w *Worker) ObserveLatency(channel string, d time.Duration) {
	if w != nil {
		w.Latency.WithLabelValues(channel).Observe(d.Seconds())
	}
}

// RateLimitedInc counts a 429.
func (w *Worker) RateLimitedInc(channel string) {
	if w != nil {
		w.RateLimited.WithLabelValues(channel).Inc()
	}
}

// SetSendRate publishes the limiter's current rate.
func (w *Worker) SetSendRate(channel string, rate int) {
	if w != nil {
		w.SendRate.WithLabelValues(channel).Set(float64(rate))
	}
}

// LLM results recorded per model call.
const (
	LLMResultOK      = "ok"      // output passed validation
	LLMResultInvalid = "invalid" // output failed validation (a repair may follow)
	LLMResultError   = "error"   // transport error, refusal, truncation
)

// API groups the API-process metrics. A nil *API is valid and records nothing.
type API struct {
	LLMCalls   *prometheus.CounterVec   // composer_llm_calls_total{purpose,result}
	LLMRepairs *prometheus.CounterVec   // composer_llm_repairs_total{purpose}
	LLMLatency *prometheus.HistogramVec // composer_llm_latency_seconds{purpose}
}

// QueueDepthRow is one (channel, status) message count.
type QueueDepthRow struct {
	Channel, Status string
	Count           int
}

// QueueDepthFunc reads the current message counts; it is called on every scrape.
type QueueDepthFunc func(ctx context.Context) ([]QueueDepthRow, error)

// NewAPI creates the API metrics and registers them, plus a
// composer_queue_depth{channel,status} gauge read through depth at scrape
// time (skipped when depth is nil), on reg.
func NewAPI(reg prometheus.Registerer, depth QueueDepthFunc) *API {
	a := &API{
		LLMCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "composer_llm_calls_total",
			Help: "LLM model calls by purpose and result (ok, invalid, error).",
		}, []string{"purpose", "result"}),
		LLMRepairs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "composer_llm_repairs_total",
			Help: "LLM repair attempts (calls after the first) by purpose.",
		}, []string{"purpose"}),
		LLMLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "composer_llm_latency_seconds",
			Help:    "LLM call latency by purpose.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 40, 80},
		}, []string{"purpose"}),
	}
	reg.MustRegister(a.LLMCalls, a.LLMRepairs, a.LLMLatency)
	for _, p := range []string{"segment", "refine", "content"} {
		for _, r := range []string{LLMResultOK, LLMResultInvalid, LLMResultError} {
			a.LLMCalls.WithLabelValues(p, r)
		}
		a.LLMRepairs.WithLabelValues(p)
	}
	if depth != nil {
		reg.MustRegister(&queueDepthCollector{read: depth})
	}
	return a
}

// LLMCall records one model call; attempt > 1 counts as a repair.
func (a *API) LLMCall(purpose, result string, attempt int, d time.Duration) {
	if a == nil {
		return
	}
	a.LLMCalls.WithLabelValues(purpose, result).Inc()
	a.LLMLatency.WithLabelValues(purpose).Observe(d.Seconds())
	if attempt > 1 {
		a.LLMRepairs.WithLabelValues(purpose).Inc()
	}
}

var queueDepthDesc = prometheus.NewDesc("composer_queue_depth",
	"Messages by channel and status, read from the database at scrape time.",
	[]string{"channel", "status"}, nil)

// queueDepthTimeout bounds the scrape-time query.
const queueDepthTimeout = 3 * time.Second

type queueDepthCollector struct{ read QueueDepthFunc }

func (c *queueDepthCollector) Describe(ch chan<- *prometheus.Desc) { ch <- queueDepthDesc }

func (c *queueDepthCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), queueDepthTimeout)
	defer cancel()
	rows, err := c.read(ctx)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(queueDepthDesc, err)
		return
	}
	for _, r := range rows {
		ch <- prometheus.MustNewConstMetric(queueDepthDesc, prometheus.GaugeValue, float64(r.Count), r.Channel, r.Status)
	}
}
