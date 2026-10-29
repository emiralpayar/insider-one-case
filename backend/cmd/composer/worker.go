package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/db"
	"github.com/emiralpayar/campaign-composer/internal/metrics"
	"github.com/emiralpayar/campaign-composer/internal/queue"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const shutdownGrace = 10 * time.Second

// runWorker starts one Dispatcher per channel, the campaign completion loop
// and the metrics/health server. SIGTERM/SIGINT stop claiming; in-flight
// sends get up to shutdownGrace to finish and be recorded.
func runWorker(dbURL string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	channels := []string{"sms", "email"}
	// Per channel: the leader's lock connection, its claim query and up to
	// senderConcurrency senders recording results; plus the campaign
	// completion loop and /healthz.
	pool, err := db.Open(ctx, withPoolSize(dbURL, len(channels)*(senderConcurrency+2)+2))
	if err != nil {
		return err
	}
	defer pool.Close()

	providerURL := envOr("PROVIDER_URL", "http://localhost:8081")
	idem := true
	if v := os.Getenv("PROVIDER_IDEMPOTENCY"); v != "" {
		if idem, err = strconv.ParseBool(v); err != nil {
			return errors.New("PROVIDER_IDEMPOTENCY must be a boolean")
		}
	}
	provider := queue.NewHTTPProvider(providerURL, idem, 5*time.Second)

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := metrics.NewWorker(reg)

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		pctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(pctx); err != nil {
			http.Error(w, "db: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: envOr("METRICS_ADDR", ":9090"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics server: %v", err)
		}
	}()

	var wg sync.WaitGroup
	for _, ch := range channels {
		d := &queue.Dispatcher{
			Pool: pool, Store: queue.NewStore(pool), Provider: provider, Channel: ch,
			Limiter: queue.NewLimiter(100, time.Now), Metrics: m, Concurrency: senderConcurrency,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.Run(ctx); err != nil {
				log.Printf("dispatcher %s: %v", ch, err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := queue.CompleteCampaigns(ctx, pool); err != nil && ctx.Err() == nil {
					log.Printf("complete campaigns: %v", err)
				} else if n > 0 {
					log.Printf("completed %d campaign(s)", n)
				}
			}
		}
	}()
	log.Printf("worker started (provider=%s idempotency=%v metrics=%s)", providerURL, idem, srv.Addr)

	<-ctx.Done()
	log.Println("worker: shutting down")
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		log.Println("worker: in-flight sends did not finish in time; leases will expire and be recovered")
	}
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// senderConcurrency is the number of parallel provider calls per channel.
const senderConcurrency = 20

// withPoolSize sets pool_max_conns unless the URL already does (the caller
// sizes it so every concurrent DB user of the worker gets a connection).
func withPoolSize(dbURL string, n int) string {
	u, err := url.Parse(dbURL)
	if err != nil || u.Scheme == "" {
		return dbURL
	}
	q := u.Query()
	if q.Get("pool_max_conns") == "" {
		q.Set("pool_max_conns", strconv.Itoa(n))
		u.RawQuery = q.Encode()
	}
	return u.String()
}
