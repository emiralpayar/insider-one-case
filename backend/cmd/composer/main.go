// Command composer is the Smart Campaign Composer entry point.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/db"
	"github.com/emiralpayar/campaign-composer/internal/seed"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: composer <migrate|seed|api|worker>")
		os.Exit(2)
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
		log.Fatalf("%s: not implemented", os.Args[1])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func mustURL(u string) {
	if u == "" {
		log.Fatal("DATABASE_URL is not set")
	}
}
