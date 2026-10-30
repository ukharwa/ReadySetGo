package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/store"
)

const (
	// pollInterval is how long an idle worker waits before looking again.
	pollInterval = time.Second
	// lease is how long a claimed job is reserved for its worker. If the
	// worker dies, another one can claim the job once the lease runs out.
	lease = 5 * time.Minute
	// jobTimeout bounds one run of a handler. It is shorter than the lease,
	// so a handler that respects its context stops before the job could be
	// claimed a second time.
	jobTimeout = 4 * time.Minute
)

// Worker claims jobs from the database and runs their handlers.
type Worker struct {
	db       *db.DB
	logger   *slog.Logger
	handlers map[string]Handler
	now      func() time.Time // replaced in tests
}

// NewWorker returns a worker with no handlers.
func NewWorker(d *db.DB, logger *slog.Logger) *Worker {
	return &Worker{db: d, logger: logger, handlers: map[string]Handler{}, now: db.Now}
}

// Handle registers h for jobs of kind. Call it before Run.
func (w *Worker) Handle(kind string, h Handler) {
	if _, dup := w.handlers[kind]; dup {
		panic("jobs: two handlers for " + kind)
	}
	w.handlers[kind] = h
}

// Run runs jobs with concurrency goroutines until ctx is cancelled. It then
// stops claiming jobs and returns once the running ones finish; those keep
// their own timeout rather than being cut off, since a job stopped halfway
// would only run again.
func (w *Worker) Run(ctx context.Context, concurrency int) {
	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			for ctx.Err() == nil {
				ran, err := w.RunOne(ctx)
				if err != nil {
					w.logger.ErrorContext(ctx, "job queue", "err", err)
				}
				if !ran {
					select {
					case <-ctx.Done():
					case <-time.After(pollInterval):
					}
				}
			}
		})
	}
	wg.Wait()
}

// RunOne claims and runs one ready job. It reports whether there was one.
// The job's own failure is handled (retried or marked failed), not
// returned; the error is about the queue itself.
func (w *Worker) RunOne(ctx context.Context) (bool, error) {
	now := w.now()
	job, err := store.New(w.db.Write).ClaimJob(ctx, store.ClaimJobParams{
		Now:         now,
		LockedUntil: sql.NullTime{Time: now.Add(lease), Valid: true},
	})
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim job: %w", err)
	}

	start := time.Now()
	runErr := w.call(ctx, job)
	log := w.logger.With("job_id", job.ID, "kind", job.Kind, "attempt", job.Attempts, "duration", time.Since(start))

	// The outcome is recorded even if ctx was cancelled meanwhile.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	q := store.New(w.db.Write)
	switch {
	case runErr == nil && job.UniqueKey.Valid:
		err = q.CompleteJob(ctx, store.CompleteJobParams{ID: job.ID, CompletedAt: nullTime(w.now())})
		log.DebugContext(ctx, "job done")
	case runErr == nil:
		err = q.DeleteJob(ctx, job.ID)
		log.DebugContext(ctx, "job done")
	case job.Attempts >= job.MaxAttempts:
		err = q.FailJob(ctx, store.FailJobParams{ID: job.ID, FailedAt: nullTime(w.now()), LastError: errorText(runErr)})
		log.ErrorContext(ctx, "job failed for good", "err", runErr)
	default:
		retryAt := w.now().Add(retryDelay(job.Attempts))
		err = q.RetryJob(ctx, store.RetryJobParams{ID: job.ID, RunAt: retryAt, LastError: errorText(runErr)})
		log.WarnContext(ctx, "job failed, will retry", "err", runErr, "retry_at", retryAt)
	}
	if err != nil {
		return true, fmt.Errorf("record outcome of job %s: %w", job.ID, err)
	}
	return true, nil
}

// call runs the job's handler, turning a panic into an error so one bad job
// can't take the worker down.
func (w *Worker) call(ctx context.Context, job store.Job) (err error) {
	h, ok := w.handlers[job.Kind]
	if !ok {
		return fmt.Errorf("no handler registered for job kind %q", job.Kind)
	}
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("panic: %v", v)
		}
	}()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobTimeout)
	defer cancel()
	return h(ctx, []byte(job.Payload))
}

// retryDelay doubles with each attempt (2s, 4s, 8s, …) up to 20 minutes,
// with jitter so failed jobs don't all retry at the same moment.
func retryDelay(attempts int64) time.Duration {
	d := min(time.Duration(1<<min(attempts, 20))*time.Second, 20*time.Minute)
	return d + rand.N(d/5+1)
}

func errorText(err error) sql.NullString {
	s := err.Error()
	if len(s) > 2000 {
		s = s[:2000]
	}
	return sql.NullString{String: s, Valid: true}
}

func nullTime(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: true} }

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
