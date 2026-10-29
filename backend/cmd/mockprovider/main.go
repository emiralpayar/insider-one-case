package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/emiralpayar/campaign-composer/internal/mockprovider"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		// Container healthcheck (the distroless image has no curl).
		c := http.Client{Timeout: 3 * time.Second}
		resp, err := c.Get("http://localhost:" + port + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}
	log.Printf("mock provider listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mockprovider.New(mockprovider.ConfigFromEnv())))
}
