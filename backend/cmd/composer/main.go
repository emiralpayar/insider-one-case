// Command composer is the Smart Campaign Composer entry point.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/api"
	"github.com/emiralpayar/campaign-composer/internal/db"
	"github.com/emiralpayar/campaign-composer/internal/llm"
	"github.com/emiralpayar/campaign-composer/internal/seed"
	"github.com/emiralpayar/campaign-composer/internal/segment"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: composer <migrate|seed|api|worker|healthcheck URL>")
		os.Exit(2)
	}
	if os.Args[1] == "healthcheck" {
		// Used by the container healthcheck (the distroless image has no curl).
		if len(os.Args) < 3 {
			log.Fatal("usage: composer healthcheck URL")
		}
		os.Exit(httpCheck(os.Args[2]))
	}
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")

	switch os.Args[1] {
	case "migrate":
		mustURL(url)
		if err := db.Migrate(ctx, url); err != nil {
			log.Fatalf("migrate: %v", err)
		}
		log.Println("migrations applied")
	case "seed":
		mustURL(url)
		if err := db.Migrate(ctx, url); err != nil {
			log.Fatalf("migrate: %v", err)
		}
		pool, err := db.Open(ctx, url)
		if err != nil {
			log.Fatalf("open: %v", err)
		}
		defer pool.Close()
		res, err := seed.Run(ctx, pool, seed.Options{Now: time.Now()})
		if err != nil {
			log.Fatalf("seed: %v", err)
		}
		log.Printf("seed: users=%d events=%d skipped=%v", res.Users, res.Events, res.Skipped)
	case "worker":
		mustURL(url)
		if err := runWorker(url); err != nil {
			log.Fatalf("worker: %v", err)
		}
	case "api":
		mustURL(url)
		if err := runAPI(url); err != nil {
			log.Fatalf("api: %v", err)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

// httpCheck returns 0 when GET u answers 2xx within 3 seconds.
func httpCheck(u string) int {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(u)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		fmt.Fprintln(os.Stderr, resp.Status)
		return 1
	}
	return 0
}

func mustURL(u string) {
	if u == "" {
		log.Fatal("DATABASE_URL is not set")
	}
}

const defaultModel = "claude-sonnet-5-5"

// selectLLM builds the Composer from the environment. LLM_PROVIDER defaults to
// anthropic; without ANTHROPIC_API_KEY the Composer is nil (LLM endpoints
// answer 503) — there is deliberately no silent fallback to the mock.
func selectLLM(pool *pgxpool.Pool) (provider string, models api.Models, c *llm.Composer, err error) {
	provider = envOr("LLM_PROVIDER", "anthropic")
	models = api.Models{
		Segment: envOr("ANTHROPIC_MODEL_SEGMENT", defaultModel),
		Content: envOr("ANTHROPIC_MODEL_CONTENT", defaultModel),
	}
	switch provider {
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			log.Printf("LLM: provider=anthropic NOT CONFIGURED (ANTHROPIC_API_KEY is empty); LLM endpoints will return 503")
			return provider, models, nil, nil
		}
		log.Printf("LLM: provider=anthropic segment_model=%s content_model=%s", models.Segment, models.Content)
		return provider, models, &llm.Composer{
			SegmentClient: llm.NewAnthropic(key, models.Segment),
			ContentClient: llm.NewAnthropic(key, models.Content),
			Log:           llm.NewPGCallLogger(pool),
			RuleCheck:     segment.IneffectiveCheck(pool),
		}, nil
	case "mock":
		models = api.Models{Segment: "mock", Content: "mock"}
		log.Printf("LLM: provider=mock (LLM_PROVIDER=mock) — deterministic fixtures, NOT a real model")
		return provider, models, &llm.Composer{
			SegmentClient: llm.NewMock(), ContentClient: llm.NewMock(), Log: llm.NewPGCallLogger(pool),
			RuleCheck: segment.IneffectiveCheck(pool),
		}, nil
	default:
		return "", models, nil, fmt.Errorf("unknown LLM_PROVIDER %q (anthropic|mock)", provider)
	}
}

// runAPI migrates, seeds an empty database and serves the HTTP API until
// SIGTERM/SIGINT.
func runAPI(dbURL string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	if err := db.Migrate(ctx, dbURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	pool, err := db.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	res, err := seed.Run(ctx, pool, seed.Options{Now: time.Now()})
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	if res.Skipped {
		log.Println("seed: database not empty, skipped")
	} else {
		log.Printf("seed: users=%d events=%d", res.Users, res.Events)
	}

	provider, models, composer, err := selectLLM(pool)
	if err != nil {
		return err
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	dupLLM := false
	if v := os.Getenv("DUPLICATE_LLM_CHECK"); v != "" {
		if dupLLM, err = strconv.ParseBool(v); err != nil {
			return errors.New("DUPLICATE_LLM_CHECK must be a boolean")
		}
	}
	log.Printf("duplicate warning: LLM similarity check %s", map[bool]string{true: "ON", false: "off (DUPLICATE_LLM_CHECK)"}[dupLLM])
	handler := api.New(api.Config{
		Pool: pool, Composer: composer, LLMProvider: provider, Models: models, Registry: reg,
		DuplicateLLMCheck: dupLLM,
	})

	srv := &http.Server{
		Addr:              ":" + envOr("PORT", "8080"),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // SSE content streams can run for a long time
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Printf("api listening on %s", srv.Addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Println("api: shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		// Long-lived SSE streams did not finish in time; cut them.
		log.Printf("api: graceful shutdown incomplete (%v); closing connections", err)
		_ = srv.Close()
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
