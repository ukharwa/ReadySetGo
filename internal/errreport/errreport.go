// Package errreport sends errors to Sentry, or to GlitchTip or Bugsink,
// which accept the same format: every record logged at error level, with
// its error, attributes and request ID.
//
// Logging an error once, where it's handled, is the convention everywhere
// (AGENTS.md), so this one hook covers panics in requests, 500 responses and
// failed jobs. Requests themselves are never sent, so cookies, passwords and
// other form fields can't leak into reports.
package errreport

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/ukharwa/kickstart/internal/logging"
)

// Handler is a slog.Handler that reports error records, then passes every
// record on to the handler it wraps.
type Handler struct {
	next   slog.Handler
	client *sentry.Client
	attrs  []slog.Attr // from WithAttrs
}

// NewHandler returns a Handler wrapping next that reports to the service
// at dsn, tagging reports with env (dev or prod).
func NewHandler(next slog.Handler, dsn, env string) (*Handler, error) {
	return newHandler(next, sentry.ClientOptions{Dsn: dsn, Environment: env})
}

func newHandler(next slog.Handler, opts sentry.ClientOptions) (*Handler, error) {
	client, err := sentry.NewClient(opts)
	if err != nil {
		return nil, err
	}
	return &Handler{next: next, client: client}, nil
}

// Enabled reports whether level is reported or logged.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelError || h.next.Enabled(ctx, level)
}

// Handle reports r if it is an error, and logs it if next would.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		h.report(ctx, r)
	}
	if !h.next.Enabled(ctx, r.Level) {
		return nil
	}
	return h.next.Handle(ctx, r)
}

// WithAttrs returns a Handler that adds attrs to every record.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs), client: h.client, attrs: append(slices.Clip(h.attrs), attrs...)}
}

// WithGroup returns a Handler whose records' attributes are in group name.
// Reports list attributes without their group.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name), client: h.client, attrs: h.attrs}
}

// report sends r: its message, its first error attribute as the exception,
// the other attributes as the "attributes" context, and the request ID as a
// tag, so a report leads to the request's log lines.
func (h *Handler) report(ctx context.Context, r slog.Record) {
	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	event.Message = r.Message
	event.Timestamp = r.Time
	attrs := sentry.Context{}
	var exception error
	add := func(a slog.Attr) bool {
		v := a.Value.Resolve()
		if err, ok := v.Any().(error); ok && exception == nil {
			exception = err
		} else {
			attrs[a.Key] = v.String()
		}
		return true
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(add)
	event.Contexts["attributes"] = attrs
	if exception != nil {
		event.SetException(exception, h.client.Options().MaxErrorDepth)
	}
	if id := logging.RequestID(ctx); id != "" {
		event.Tags["request_id"] = id
	}
	h.client.CaptureEvent(event, nil, nil)
}

// Flush waits up to two seconds for reports still being sent, if logger
// reports errors. Call it before the program exits.
func Flush(logger *slog.Logger) {
	if h, ok := logger.Handler().(*Handler); ok {
		h.client.Flush(2 * time.Second)
	}
}
