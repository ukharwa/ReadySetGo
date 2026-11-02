// Package emailtest provides a Sender for tests.
package emailtest

import (
	"context"
	"slices"
	"sync"

	"github.com/ukharwa/readysetgo/internal/email"
)

// Recorder is an email.Sender that keeps every message for tests to
// inspect.
type Recorder struct {
	mu       sync.Mutex
	messages []email.Message
}

// Send implements email.Sender.
func (r *Recorder) Send(_ context.Context, m email.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, m)
	return nil
}

// Messages returns the messages sent so far.
func (r *Recorder) Messages() []email.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.messages)
}
