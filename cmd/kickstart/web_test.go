package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ukharwa/kickstart/internal/apptest"
	"github.com/ukharwa/kickstart/internal/config"
	"github.com/ukharwa/kickstart/internal/httpx"
)

func TestRoutesHomeAndStatic(t *testing.T) {
	t.Parallel()
	a := apptest.New(t)
	h, err := routes(config.Config{Env: "prod", AppName: "Test"}, slog.New(slog.DiscardHandler), &httpx.Health{}, a.Accounts, a.Limiter)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/static/app.css?v=") {
		t.Errorf("home: status %d, want 200 linking the versioned stylesheet", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/htmx.min.js?v=nope", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("static file with a stale version: status %d, nosniff %q, Cache-Control %q",
			rec.Code, rec.Header().Get("X-Content-Type-Options"), rec.Header().Get("Cache-Control"))
	}
}
