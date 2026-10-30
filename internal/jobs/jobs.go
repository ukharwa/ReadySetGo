// Package jobs is the application's background job queue, kept in the
// database: Enqueue adds a job, a Worker runs them, and a Scheduler enqueues
// recurring ones.
//
// Jobs run at least once. A job can run again after a crash, a timeout or a
// lost lease, so every handler must be safe to repeat.
//
// The package API (Enqueue, Worker, Scheduler) is the seam for moving to a
// dedicated queue later (River, NATS, …); SCALING.md describes how. It isn't
// a Go interface because Enqueue joins the caller's database transaction,
// which only a database-backed queue can do.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/store"
)

// maxAttempts is how often a job runs before it is marked failed. With the
// backoff in retryDelay, the retries span about 17 minutes.
const maxAttempts = 10

// Handler runs one job of a kind, given its JSON payload. Returning an error
// schedules a retry.
type Handler func(ctx context.Context, payload []byte) error

// Enqueue adds a job of kind, with payload marshalled to JSON, to run as
// soon as a worker is free. Pass store.New(tx) inside db.InTx to enqueue in
// the same transaction as the change that caused the job: it then exists
// only if that change commits.
func Enqueue(ctx context.Context, q *store.Queries, kind string, payload any) error {
	_, err := insert(ctx, q, kind, payload, "", db.Now())
	return err
}

// insert adds a job, skipping it if uniqueKey is set and already taken. It
// reports whether the job was added.
func insert(ctx context.Context, q *store.Queries, kind string, payload any, uniqueKey string, runAt time.Time) (bool, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("enqueue %s: marshal payload: %w", kind, err)
	}
	n, err := q.InsertJob(ctx, store.InsertJobParams{
		ID:          db.NewID(),
		Kind:        kind,
		Payload:     string(b),
		UniqueKey:   nullString(uniqueKey),
		RunAt:       runAt,
		MaxAttempts: maxAttempts,
		CreatedAt:   db.Now(),
	})
	if err != nil {
		return false, fmt.Errorf("enqueue %s: %w", kind, err)
	}
	return n > 0, nil
}
