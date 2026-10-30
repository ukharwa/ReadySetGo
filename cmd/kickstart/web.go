package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/ukharwa/kickstart/internal/auth"
	"github.com/ukharwa/kickstart/internal/config"
	"github.com/ukharwa/kickstart/internal/httpx"
	"github.com/ukharwa/kickstart/internal/ratelimit"
	"github.com/ukharwa/kickstart/internal/web"
)

// mountPages registers the HTML pages: static files on root (outside the
// middleware stack, since they need neither the access log nor a session
// lookup), and every feature's pages on app.
func mountPages(cfg config.Config, logger *slog.Logger, root, app *http.ServeMux, accounts *auth.Service, limiter ratelimit.Limiter) error {
	pages, err := web.New(web.Options{AppName: cfg.AppName, Dev: cfg.Dev(), User: currentUser, Logger: logger})
	if err != nil {
		return err
	}
	root.Handle("GET /static/", httpx.SecurityHeaders(!cfg.Dev())(pages.Static()))

	home, err := pages.Home()
	if err != nil {
		return err
	}
	app.Handle("GET /{$}", home)

	accountPages, err := auth.NewWeb(accounts, pages, limiter)
	if err != nil {
		return err
	}
	accountPages.Routes(app)
	return nil
}

// currentUser gives the page layout the logged-in user, or nil.
func currentUser(ctx context.Context) any {
	if u, ok := auth.UserFrom(ctx); ok {
		return &u
	}
	return nil
}
