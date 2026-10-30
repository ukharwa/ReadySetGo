package email_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/ukharwa/kickstart/internal/db/dbtest"
	"github.com/ukharwa/kickstart/internal/email"
	"github.com/ukharwa/kickstart/internal/email/emailtest"
	"github.com/ukharwa/kickstart/internal/jobs"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	ok := email.Message{To: "ada@example.com", Subject: "Hi", Body: "Hello"}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid message: %v", err)
	}
	for name, m := range map[string]email.Message{
		"header injection": {To: "ada@example.com", Subject: "Hi\r\nBcc: eve@example.com", Body: "x"},
		"two recipients":   {To: "ada@example.com, eve@example.com", Subject: "Hi", Body: "x"},
		"display name":     {To: "Ada <ada@example.com>", Subject: "Hi", Body: "x"},
		"not an address":   {To: "ada", Subject: "Hi", Body: "x"},
		"no body":          {To: "ada@example.com", Subject: "Hi"},
	} {
		if err := m.Validate(); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestQueuedEmailIsSent follows an email from Enqueue through the worker to
// the sender.
func TestQueuedEmailIsSent(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	rec := &emailtest.Recorder{}
	w := jobs.NewWorker(d, slog.New(slog.DiscardHandler))
	w.Handle(email.JobKind, email.Handler(rec))

	m := email.Message{To: "ada@example.com", Subject: "Welcome", Body: "Hello, Ada."}
	if err := email.Enqueue(t.Context(), d.Write, m); err != nil {
		t.Fatal(err)
	}
	if len(rec.Messages()) != 0 {
		t.Fatal("email sent before the worker ran")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for len(rec.Messages()) == 0 && ctx.Err() == nil {
		if _, err := w.RunOne(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := rec.Messages(); len(got) != 1 || got[0] != m {
		t.Errorf("sent %+v, want [%+v]", got, m)
	}
}

func TestEnqueueRejectsInvalid(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	err := email.Enqueue(t.Context(), d.Write, email.Message{To: "nope", Subject: "x", Body: "x"})
	if err == nil {
		t.Error("Enqueue accepted an invalid message")
	}
}
