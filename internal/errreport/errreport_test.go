package errreport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/ukharwa/kickstart/internal/logging"
)

// transport keeps the events it is given instead of sending them.
type transport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (t *transport) Configure(sentry.ClientOptions)        {}
func (t *transport) Flush(time.Duration) bool              { return true }
func (t *transport) FlushWithContext(context.Context) bool { return true }
func (t *transport) Close()                                {}
func (t *transport) SendEvent(e *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, e)
}

func TestHandler(t *testing.T) {
	t.Parallel()
	sent := &transport{}
	h, err := newHandler(slog.DiscardHandler, sentry.ClientOptions{Dsn: "https://key@errors.example.com/1", Transport: sent})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(h).With("component", "test")
	ctx := logging.WithRequestID(t.Context(), "REQ1")

	logger.InfoContext(ctx, "not an error")
	cause := errors.New("connection refused")
	logger.ErrorContext(ctx, "load user", "err", fmt.Errorf("query: %w", cause), "user_id", 42)
	Flush(logger)

	if len(sent.events) != 1 {
		t.Fatalf("%d events sent, want only the error", len(sent.events))
	}
	e := sent.events[0]
	attrs := e.Contexts["attributes"]
	if e.Message != "load user" || e.Tags["request_id"] != "REQ1" || attrs["user_id"] != "42" || attrs["component"] != "test" {
		t.Errorf("event: message %q, tags %v, attributes %v", e.Message, e.Tags, attrs)
	}
	if len(e.Exception) == 0 || e.Exception[len(e.Exception)-1].Value != "query: connection refused" {
		t.Errorf("exception: %+v", e.Exception)
	}
}
