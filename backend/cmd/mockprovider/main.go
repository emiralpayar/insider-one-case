package main

import (
	"log"
	"net/http"
	"os"

	"github.com/emiralpayar/campaign-composer/internal/mockprovider"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	log.Printf("mock provider listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mockprovider.New(mockprovider.ConfigFromEnv())))
}
