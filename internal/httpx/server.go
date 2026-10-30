// Package httpx holds the HTTP server setup and helpers shared by every
// feature's handlers.
package httpx

import (
	"log/slog"
	"net/http"
	"time"
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
