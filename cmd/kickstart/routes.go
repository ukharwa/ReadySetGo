package main

import (
	"log/slog"
	"net/http"

	// kickstart:begin ui=api
	"github.com/ukharwa/kickstart/internal/auth"
	// kickstart:end
	"github.com/ukharwa/kickstart/internal/config"
	"github.com/ukharwa/kickstart/internal/httpx"

	// kickstart:begin ui=api
	// kickstart:begin module=admin
	"github.com/ukharwa/kickstart/internal/admin"
	// kickstart:end
	// kickstart:begin module=audit
	"github.com/ukharwa/kickstart/internal/audit"
	// kickstart:end
	// kickstart:begin module=uploads
	"github.com/ukharwa/kickstart/internal/uploads"
	// kickstart:end
	// kickstart:begin module=workspaces
	"github.com/ukharwa/kickstart/internal/workspaces"
	// kickstart:end
	// kickstart:end
)

// routes returns the application's handler. Each feature registers its own
// routes (see its Routes method); this is where they are all mounted, so
// adding or removing a feature is a line here.
func routes(cfg config.Config, logger *slog.Logger, health *httpx.Health, svc *services) (http.Handler, error) {
	app := http.NewServeMux()
	app.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { httpx.Error(w, r, http.StatusNotFound) })

	// Health checks skip the middleware stack: load balancers and uptime
	// monitors poll them every few seconds, which would flood the access log.
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", health.Live)
	root.HandleFunc("GET /readyz", health.Ready)

	// kickstart:begin ui=html
	if err := mountPages(cfg, logger, root, app, svc); err != nil {
		return nil, err
	}
	// kickstart:end
	// kickstart:begin ui=api
	auth.NewAPI(svc.accounts, svc.limiter).Routes(app)
	// kickstart:begin module=workspaces
	workspaces.NewAPI(svc.workspaces).Routes(app)
	// kickstart:end
	// kickstart:begin module=audit
	audit.NewAPI(svc.audit).Routes(app)
	// kickstart:end
	// kickstart:begin module=admin
	admin.NewAPI(svc.admin).Routes(app)
	// kickstart:end
	// kickstart:begin module=uploads
	uploads.NewAPI(svc.uploads).Routes(app)
	// kickstart:end
	// kickstart:end

	stack := append(httpx.Stack(logger, cfg.TrustedProxies, !cfg.Dev()), svc.accounts.LoadUser)
	// kickstart:begin module=metrics
	stack = append(stack, svc.metrics.Middleware) // last: it reads the route the router matched
	// kickstart:end
	root.Handle("/", httpx.Chain(app, stack...))
	return root, nil
}
