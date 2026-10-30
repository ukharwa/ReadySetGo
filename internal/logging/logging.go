// Package logging builds the application's structured logger.
package logging

import (
	"io"
	"log/slog"
)

// New returns a logger that writes to w as JSON when format is "json" and as
// human-readable text otherwise.
func New(w io.Writer, format string, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}
