package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func status(h http.HandlerFunc) int {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Code
}

func TestHealth(t *testing.T) {
	t.Parallel()
	h := &Health{}

	if got := status(h.Live); got != http.StatusOK {
		t.Errorf("live: %d, want 200", got)
	}
	if got := status(h.Ready); got != http.StatusOK {
		t.Errorf("ready: %d, want 200", got)
	}

	h.SetShuttingDown()
	if got := status(h.Live); got != http.StatusOK {
		t.Errorf("live while shutting down: %d, want 200", got)
	}
	if got := status(h.Ready); got != http.StatusServiceUnavailable {
		t.Errorf("ready while shutting down: %d, want 503", got)
	}
}

func TestReadyCheckFails(t *testing.T) {
	t.Parallel()
	h := &Health{Check: func(context.Context) error { return errors.New("database down") }}
	if got := status(h.Ready); got != http.StatusServiceUnavailable {
		t.Errorf("ready with failing check: %d, want 503", got)
	}
	if got := status(h.Live); got != http.StatusOK {
		t.Errorf("live with failing check: %d, want 200", got)
	}
}
