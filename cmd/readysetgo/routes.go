package main

import (
	"log/slog"
	"net/http"

	// readysetgo:begin ui=api
	"github.com/ukharwa/readysetgo/internal/auth"
	// readysetgo:end
	"github.com/ukharwa/readysetgo/internal/config"
	"github.com/ukharwa/readysetgo/internal/httpx"

	// readysetgo:begin ui=api
	// readysetgo:begin module=admin
	"github.com/ukharwa/readysetgo/internal/admin"
	// readysetgo:end
	// readysetgo:begin module=audit
	"github.com/ukharwa/readysetgo/internal/audit"
	// readysetgo:end
	// readysetgo:begin module=uploads
	"github.com/ukharwa/readysetgo/internal/uploads"
	// readysetgo:end
	// readysetgo:begin module=twofactor
	"github.com/ukharwa/readysetgo/internal/twofactor"
	// readysetgo:end
	// readysetgo:begin module=workspaces
	"github.com/ukharwa/readysetgo/internal/workspaces"
	// readysetgo:end
	// readysetgo:end

	// readysetgo:begin ui=api
	// example:begin
	"github.com/ukharwa/readysetgo/internal/notes"
	// example:end
	// readysetgo:end
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

	// readysetgo:begin ui=html
	if err := mountPages(cfg, logger, root, app, svc); err != nil {
		return nil, err
	}
	// readysetgo:end
	// readysetgo:begin ui=api
	auth.NewAPI(svc.accounts, svc.limiter).Routes(app)
	// example:begin
	notes.NewAPI(svc.notes).Routes(app)
	// example:end
	// readysetgo:begin module=workspaces
	workspaces.NewAPI(svc.workspaces).Routes(app)
	// readysetgo:end
	// readysetgo:begin module=audit
	audit.NewAPI(svc.audit).Routes(app)
	// readysetgo:end
	// readysetgo:begin module=admin
	admin.NewAPI(svc.admin).Routes(app)
	// readysetgo:end
	// readysetgo:begin module=uploads
	uploads.NewAPI(svc.uploads).Routes(app)
	// readysetgo:end
	// readysetgo:begin module=twofactor
	twofactor.NewAPI(svc.twoFactor, svc.limiter).Routes(app)
	// readysetgo:end
	// readysetgo:end

	stack := append(httpx.Stack(logger, cfg.TrustedProxies, !cfg.Dev()), svc.accounts.LoadUser)
	// readysetgo:begin module=metrics
	stack = append(stack, svc.metrics.Middleware) // last: it reads the route the router matched
	// readysetgo:end
	root.Handle("/", httpx.Chain(app, stack...))
	return root, nil
}
