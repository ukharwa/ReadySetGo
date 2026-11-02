package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/httpx"
)

func TestRoutesHomeAndStatic(t *testing.T) {
	t.Parallel()
	a := apptest.New(t)
	cfg := testConfig(t)
	logger := slog.New(slog.DiscardHandler)
	svc, err := newServices(cfg, a.DB, logger)
	if err != nil {
		t.Fatal(err)
	}
	h, err := routes(cfg, logger, &httpx.Health{}, svc)
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
