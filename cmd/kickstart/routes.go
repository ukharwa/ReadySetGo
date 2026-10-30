package main

import (
	"log/slog"
	"net/http"

	"github.com/ukharwa/kickstart/internal/config"
	"github.com/ukharwa/kickstart/internal/httpx"
)

// routes returns the application's handler. Every route is registered here
// so the whole URL space can be read in one place.
func routes(cfg config.Config, logger *slog.Logger, health *httpx.Health) http.Handler {
	app := http.NewServeMux()

	// Health checks skip the middleware below: load balancers and uptime
	// monitors poll them every few seconds, which would flood the access log.
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", health.Live)
	root.HandleFunc("GET /readyz", health.Ready)
	root.Handle("/", httpx.Chain(app,
		httpx.RealIP(cfg.TrustedProxies),
		httpx.RequestID,
		httpx.AccessLog(logger),
		httpx.Recover(logger),
		httpx.SecurityHeaders(!cfg.Dev()),
		httpx.CrossOrigin(logger),
		httpx.MaxBytes(httpx.DefaultMaxBodyBytes),
		httpx.Timeout(httpx.RequestTimeout),
	))
	return root
}
