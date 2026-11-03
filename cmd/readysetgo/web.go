package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/config"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/web"

	// readysetgo:begin module=admin
	"github.com/ukharwa/readysetgo/internal/admin"
	// readysetgo:end
	// readysetgo:begin module=audit
	"github.com/ukharwa/readysetgo/internal/audit"
	// readysetgo:end
	// readysetgo:begin module=social
	"github.com/ukharwa/readysetgo/internal/social"
	// readysetgo:end
	// readysetgo:begin module=twofactor
	"github.com/ukharwa/readysetgo/internal/twofactor"
	// readysetgo:end
	// readysetgo:begin module=uploads
	"github.com/ukharwa/readysetgo/internal/uploads"
	// readysetgo:end
	// readysetgo:begin module=workspaces
	"github.com/ukharwa/readysetgo/internal/workspaces"
	// readysetgo:end

	// example:begin
	"github.com/ukharwa/readysetgo/internal/notes"
	// example:end
)

// mountPages registers the HTML pages: static files on root (outside the
// middleware stack, since they need neither the access log nor a session
// lookup), and every feature's pages on app.
func mountPages(cfg config.Config, logger *slog.Logger, root, app *http.ServeMux, svc *services) error {
	opts := web.Options{
		AppName: cfg.AppName, Dev: cfg.Dev(), OpenSignup: !svc.accounts.InviteOnly(),
		User: currentUser, TimeZone: currentTimeZone, Logger: logger,
	}
	// readysetgo:begin module=social
	for _, p := range svc.social.Providers() {
		opts.Providers = append(opts.Providers, web.Provider(p))
	}
	// readysetgo:end
	pages, err := web.New(opts)
	if err != nil {
		return err
	}
	root.Handle("GET /static/", httpx.SecurityHeaders(!cfg.Dev())(pages.Static()))

	home, err := pages.Home()
	if err != nil {
		return err
	}
	app.Handle("GET /{$}", home)

	accountPages, err := auth.NewWeb(svc.accounts, pages, svc.limiter)
	if err != nil {
		return err
	}
	accountPages.Routes(app)
	// example:begin
	notePages, err := notes.NewWeb(svc.notes, pages)
	if err != nil {
		return err
	}
	notePages.Routes(app)
	// example:end

	// readysetgo:begin module=workspaces
	workspacePages, err := workspaces.NewWeb(svc.workspaces, pages)
	if err != nil {
		return err
	}
	workspacePages.Routes(app)
	// readysetgo:end
	// readysetgo:begin module=audit
	auditPages, err := audit.NewWeb(svc.audit, pages)
	if err != nil {
		return err
	}
	auditPages.Routes(app)
	// readysetgo:end
	// readysetgo:begin module=admin
	adminPages, err := admin.NewWeb(svc.admin, pages)
	if err != nil {
		return err
	}
	adminPages.Routes(app)
	// readysetgo:end
	// readysetgo:begin module=uploads
	uploadPages, err := uploads.NewWeb(svc.uploads, pages)
	if err != nil {
		return err
	}
	uploadPages.Routes(app)
	// readysetgo:end
	// readysetgo:begin module=twofactor
	twoFactorPages, err := twofactor.NewWeb(svc.twoFactor, pages, svc.limiter)
	if err != nil {
		return err
	}
	twoFactorPages.Routes(app)
	// readysetgo:end
	// readysetgo:begin module=social
	socialPages, err := social.NewWeb(svc.social, pages, svc.limiter)
	if err != nil {
		return err
	}
	socialPages.Routes(app)
	// readysetgo:end
	return nil
}

// currentUser gives the page layout the logged-in user, or nil.
func currentUser(ctx context.Context) any {
	if u, ok := auth.UserFrom(ctx); ok {
		return &u
	}
	return nil
}

// currentTimeZone gives pages the logged-in user's time zone, or nil (UTC)
// for a visitor.
func currentTimeZone(ctx context.Context) *time.Location {
	if u, ok := auth.UserFrom(ctx); ok {
		return u.Location()
	}
	return nil
}
