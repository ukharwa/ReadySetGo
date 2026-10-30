package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ukharwa/kickstart/internal/apptest"
	"github.com/ukharwa/kickstart/internal/config"
	"github.com/ukharwa/kickstart/internal/httpx"
)

// TestRoutesMiddleware checks that application routes get the middleware
// stack and health checks bypass it.
func TestRoutesMiddleware(t *testing.T) {
	t.Parallel()
	a := apptest.New(t)
	h, err := routes(config.Config{Env: "prod", AppName: "Test"}, slog.New(slog.DiscardHandler), &httpx.Health{}, a.Accounts, a.Limiter)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/no-such-page", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown page: status %d, want 404", rec.Code)
	}
	for _, header := range []string{"X-Request-ID", "Content-Security-Policy", "Strict-Transport-Security", "X-Content-Type-Options"} {
		if rec.Header().Get(header) == "" {
			t.Errorf("application response lacks %s", header)
		}
	}

	r := httptest.NewRequest(http.MethodPost, "/no-such-page", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site POST: status %d, want 403", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("X-Request-ID") != "" {
		t.Errorf("healthz: status %d, X-Request-ID %q; want 200 without middleware", rec.Code, rec.Header().Get("X-Request-ID"))
	}
}
