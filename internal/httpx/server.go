// Package httpx holds the HTTP server setup and helpers shared by every
// feature's handlers.
package httpx

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// NewServer returns an http.Server for h with timeouts that protect against
// slow or stalled clients. Body size limits are applied per route by
// middleware, not here.
func NewServer(h http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second, // stops slow-header (Slowloris) attacks
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
}

// PathID parses the UUID in the path parameter name, such as {noteID}. A
// handler answers 404 when it reports false.
func PathID(r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	return id, err == nil
}
