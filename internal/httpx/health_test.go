package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	t.Parallel()
	h := &Health{}

	check := func(name string, handler http.HandlerFunc, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d", name, rec.Code, want)
		}
	}

	check("live", h.Live, http.StatusOK)
	check("ready", h.Ready, http.StatusOK)

	h.SetShuttingDown()
	check("live while shutting down", h.Live, http.StatusOK)
	check("ready while shutting down", h.Ready, http.StatusServiceUnavailable)
}
