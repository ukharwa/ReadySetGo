package main

import (
	"log/slog"
	"net/http"

	"github.com/ukharwa/kickstart/internal/auth"
	"github.com/ukharwa/kickstart/internal/config"
	"github.com/ukharwa/kickstart/internal/httpx"
	"github.com/ukharwa/kickstart/internal/ratelimit"
)

// routes returns the application's handler. Each feature registers its own
// routes (see its Routes method); this is where they are all mounted, so
// adding or removing a feature is a line here.
func routes(cfg config.Config, logger *slog.Logger, health *httpx.Health, accounts *auth.Service, limiter ratelimit.Limiter) (http.Handler, error) {
	app := http.NewServeMux()
	app.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { httpx.Error(w, r, http.StatusNotFound) })

	// Health checks skip the middleware stack: load balancers and uptime
	// monitors poll them every few seconds, which would flood the access log.
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", health.Live)
	root.HandleFunc("GET /readyz", health.Ready)

	// kickstart:begin ui=html
	if err := mountPages(cfg, logger, root, app, accounts, limiter); err != nil {
		return nil, err
	}
	// kickstart:end
	// kickstart:begin ui=api
	auth.NewAPI(accounts, limiter).Routes(app)
	// kickstart:end

	stack := append(httpx.Stack(logger, cfg.TrustedProxies, !cfg.Dev()), accounts.LoadUser)
	root.Handle("/", httpx.Chain(app, stack...))
	return root, nil
}
