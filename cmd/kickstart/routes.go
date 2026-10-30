package main

import (
	"net/http"

	"github.com/ukharwa/kickstart/internal/httpx"
)

// routes returns the application's handler. Every route is registered here
// so the whole URL space can be read in one place.
func routes(health *httpx.Health) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready)
	return mux
}
