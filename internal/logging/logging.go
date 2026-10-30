// Package logging builds the application's structured logger and carries the
// request ID through contexts, so every log line about a request can be
// found by that ID.
package logging

import (
	"context"
	"io"
	"log/slog"
)

// New returns a logger that writes to w as JSON when format is "json" and as
// human-readable text otherwise. Records logged with a context carrying a
// request ID (the slog …Context methods) get a request_id attribute.
func New(w io.Writer, format string, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewTextHandler(w, opts)
	if format == "json" {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(contextHandler{h})
}

type requestIDKey struct{}

// WithRequestID returns a copy of ctx carrying the request ID id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request ID carried by ctx, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// contextHandler adds the request ID from the record's context.
type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
