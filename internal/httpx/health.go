package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// Health serves the liveness (/healthz) and readiness (/readyz) endpoints.
// Responses are plain text with no details, so both are safe to expose.
type Health struct {
	// Check, if set, must succeed for the app to count as ready. It gets a
	// context with a 2-second timeout.
	Check func(context.Context) error

	shuttingDown atomic.Bool
}

// Live reports that the process is up. It checks nothing else, so uptime
// monitors only alert when the app itself is down.
func (h *Health) Live(w http.ResponseWriter, _ *http.Request) {
	writeText(w, http.StatusOK, "ok")
}

// Ready reports whether the app can serve traffic. Load balancers use it to
// stop sending requests to an instance that is shutting down or can't reach
// its database.
func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	if h.shuttingDown.Load() {
		writeText(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if h.Check != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := h.Check(ctx); err != nil {
			slog.WarnContext(ctx, "readiness check failed", "err", err)
			writeText(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
	}
	writeText(w, http.StatusOK, "ok")
}

// SetShuttingDown makes Ready fail from now on.
func (h *Health) SetShuttingDown() { h.shuttingDown.Store(true) }

func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
