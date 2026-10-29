// Package metrics holds the Prometheus instruments exposed by the worker.
package metrics

import (
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
