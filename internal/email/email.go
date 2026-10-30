// Package email sends the application's email: plain text, through a
// Sender (SMTP in production, the log in development, a recorder in tests).
//
// Features don't send email during a request. They Enqueue it, and the job
// queue's worker sends it: requests stay fast, failures are retried, and a
// request's response time can't reveal whether an email was sent.
package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"

	"github.com/ukharwa/kickstart/internal/jobs"
	"github.com/ukharwa/kickstart/internal/store"
)

// Message is one plain-text email.
type Message struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Sender delivers email.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// JobKind is the job that sends a queued email.
const JobKind = "email.send"

// Enqueue queues m for the worker to send. Use store.New(tx) inside db.InTx
// to send it only if the surrounding change commits.
//
// The job holds the whole message, so a link with a token in it sits in the
// jobs table until the email is sent (usually seconds).
func Enqueue(ctx context.Context, q *store.Queries, m Message) error {
	if err := m.Validate(); err != nil {
		return err
	}
	return jobs.Enqueue(ctx, q, JobKind, m)
}

// Handler returns the job handler for JobKind, which sends with s.
func Handler(s Sender) jobs.Handler {
	return func(ctx context.Context, payload []byte) error {
		var m Message
		if err := json.Unmarshal(payload, &m); err != nil {
			return fmt.Errorf("decode email: %w", err)
		}
		return s.Send(ctx, m)
	}
}

// Validate checks that m can be sent safely: one valid recipient address,
// and no line breaks in the subject, which could inject extra headers.
func (m Message) Validate() error {
	var errs []error
	if a, err := mail.ParseAddress(m.To); err != nil || a.Name != "" {
		errs = append(errs, errors.New("email: To must be a single bare address"))
	}
	if strings.ContainsAny(m.Subject, "\r\n") {
		errs = append(errs, errors.New("email: subject contains a line break"))
	}
	if m.Subject == "" || m.Body == "" {
		errs = append(errs, errors.New("email: subject and body are required"))
	}
	return errors.Join(errs...)
}

// Log is a Sender for development without a mail server: it logs each
// email, body and links included, instead of sending it. Configuration
// refuses it in production.
type Log struct {
	Logger *slog.Logger
}

// Send implements Sender.
func (l Log) Send(ctx context.Context, m Message) error {
	l.Logger.InfoContext(ctx, "email not sent: no SMTP server configured",
		"to", m.To, "subject", m.Subject, "body", m.Body)
	return nil
}
