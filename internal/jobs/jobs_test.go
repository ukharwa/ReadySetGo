package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/db/dbtest"
	"github.com/ukharwa/kickstart/internal/jobs/store"
)

var discard = slog.New(slog.DiscardHandler)

// testWorker returns a worker whose clock the test controls. It starts a
// minute ahead, so jobs enqueued by the test are already due.
func testWorker(t *testing.T, d *db.DB) (*Worker, *time.Time) {
	t.Helper()
	now := db.Now().Add(time.Minute)
	w := NewWorker(d, discard)
	w.now = func() time.Time { return now }
	return w, &now
}

func countJobs(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM jobs").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func runOne(t *testing.T, w *Worker) bool {
	t.Helper()
	ran, err := w.RunOne(t.Context())
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	return ran
}

func TestEnqueueAndRun(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	w, _ := testWorker(t, d)
	var got struct{ To string }
	w.Handle("greet", func(_ context.Context, payload []byte) error {
		return json.Unmarshal(payload, &got)
	})

	if err := Enqueue(t.Context(), d.Write, "greet", map[string]string{"To": "ada"}); err != nil {
		t.Fatal(err)
	}
	if !runOne(t, w) {
		t.Fatal("RunOne found no job")
	}
	if got.To != "ada" {
		t.Errorf("handler got payload %+v", got)
	}
	if n := countJobs(t, d); n != 0 {
		t.Errorf("%d jobs left after success, want 0 (deleted)", n)
	}
	if runOne(t, w) {
		t.Error("RunOne ran a job from an empty queue")
	}
}

// TestEnqueueInTransaction checks that a job enqueued in a transaction that
// rolls back never exists.
func TestEnqueueInTransaction(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	_ = d.InTx(t.Context(), func(tx *sql.Tx) error {
		if err := Enqueue(t.Context(), tx, "greet", nil); err != nil {
			t.Fatal(err)
		}
		return errors.New("roll back")
	})
	if n := countJobs(t, d); n != 0 {
		t.Errorf("%d jobs after rollback, want 0", n)
	}
}

func TestRetryThenFail(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	w, now := testWorker(t, d)
	calls := 0
	w.Handle("flaky", func(context.Context, []byte) error { calls++; return errors.New("smtp down") })
	if err := Enqueue(t.Context(), d.Write, "flaky", nil); err != nil {
		t.Fatal(err)
	}

	runOne(t, w)
	if runOne(t, w) {
		t.Fatal("a failed job ran again before its retry time")
	}
	for range maxAttempts - 1 {
		*now = now.Add(time.Hour) // past any retry delay
		if !runOne(t, w) {
			t.Fatal("retry not run")
		}
	}
	if calls != maxAttempts {
		t.Errorf("handler called %d times, want %d", calls, maxAttempts)
	}

	*now = now.Add(time.Hour)
	if runOne(t, w) {
		t.Error("job ran after its last attempt")
	}
	var failed sql.NullTime
	var lastError sql.NullString
	if err := d.Read.QueryRowContext(t.Context(), "SELECT failed_at, last_error FROM jobs").Scan(&failed, &lastError); err != nil {
		t.Fatal(err)
	}
	if !failed.Valid || lastError.String != "smtp down" {
		t.Errorf("failed_at = %v, last_error = %q; want set and %q", failed, lastError.String, "smtp down")
	}
}

func TestPanicAndUnknownKind(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	w, _ := testWorker(t, d)
	w.Handle("boom", func(context.Context, []byte) error { panic("boom") })
	for _, kind := range []string{"boom", "nobody-handles-this"} {
		if err := Enqueue(t.Context(), d.Write, kind, nil); err != nil {
			t.Fatal(err)
		}
	}
	runOne(t, w)
	runOne(t, w)

	var withErrors int
	if err := d.Read.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM jobs WHERE last_error IS NOT NULL").Scan(&withErrors); err != nil {
		t.Fatal(err)
	}
	if withErrors != 2 {
		t.Errorf("%d jobs recorded an error, want 2 (both scheduled for retry)", withErrors)
	}
}

// TestLeaseExpiry checks that a job whose worker died is claimed again once
// its lease runs out, and not before.
func TestLeaseExpiry(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	w, now := testWorker(t, d)
	w.Handle("work", func(context.Context, []byte) error { return nil })
	if err := Enqueue(t.Context(), d.Write, "work", nil); err != nil {
		t.Fatal(err)
	}
	// A worker claims the job, then dies without finishing.
	if _, err := store.New(d.Write).ClaimJob(t.Context(), store.ClaimJobParams{
		Now: *now, LockedUntil: nullTime(now.Add(lease)),
	}); err != nil {
		t.Fatal(err)
	}

	if runOne(t, w) {
		t.Fatal("job claimed while its lease was still held")
	}
	*now = now.Add(lease + time.Second)
	if !runOne(t, w) {
		t.Fatal("job not claimed after its lease ran out")
	}
}

// TestConcurrentWorkers runs several workers against one queue, as several
// instances would, and checks every job runs exactly once.
func TestConcurrentWorkers(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	const jobs = 40
	var runs sync.Map
	var total atomic.Int64
	handler := func(_ context.Context, payload []byte) error {
		if _, dup := runs.LoadOrStore(string(payload), true); dup {
			t.Errorf("job %s ran twice", payload)
		}
		total.Add(1)
		return nil
	}
	for i := range jobs {
		if err := Enqueue(t.Context(), d.Write, "work", i); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	for range 3 {
		w := NewWorker(d, discard)
		w.Handle("work", handler)
		wg.Go(func() { w.Run(ctx, 4) })
	}
	deadline := time.Now().Add(20 * time.Second)
	for total.Load() < jobs && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	if total.Load() != jobs {
		t.Errorf("%d jobs ran, want %d", total.Load(), jobs)
	}
}

// TestRunWaitsForRunningJob checks graceful shutdown: cancelling Run stops
// new claims but lets the running job finish.
func TestRunWaitsForRunningJob(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	w := NewWorker(d, discard)
	started, finished := make(chan struct{}), make(chan struct{})
	w.Handle("slow", func(context.Context, []byte) error {
		close(started)
		time.Sleep(200 * time.Millisecond)
		close(finished)
		return nil
	})
	if err := Enqueue(t.Context(), d.Write, "slow", nil); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { w.Run(ctx, 1); close(done) }()
	<-started
	cancel()
	<-done
	select {
	case <-finished:
	default:
		t.Error("Run returned before the running job finished")
	}
	if n := countJobs(t, d); n != 0 {
		t.Errorf("%d jobs left, want 0: the finished job's outcome must still be recorded", n)
	}
}

// TestSchedulerOncePerInterval simulates two instances' schedulers: each run
// of a task must be enqueued once, and not again after it completes.
func TestSchedulerOncePerInterval(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	now := time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	a, b := NewScheduler(d, discard), NewScheduler(d, discard)
	for _, s := range []*Scheduler{a, b} {
		s.now = clock
		s.Every(time.Hour, "hourly")
	}
	w, _ := testWorker(t, d)
	w.now = clock
	runs := 0
	w.Handle("hourly", func(context.Context, []byte) error { runs++; return nil })

	a.EnqueueDue(t.Context())
	b.EnqueueDue(t.Context())
	for runOne(t, w) {
	}
	b.EnqueueDue(t.Context()) // after the run completed: still the same hour
	for runOne(t, w) {
	}
	if runs != 1 {
		t.Fatalf("10:00 run happened %d times, want 1", runs)
	}

	now = now.Add(time.Hour)
	a.EnqueueDue(t.Context())
	b.EnqueueDue(t.Context())
	for runOne(t, w) {
	}
	if runs != 2 {
		t.Errorf("after the next hour: %d runs, want 2", runs)
	}
}

func TestCleanup(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	ctx := t.Context()
	q := store.New(d.Write)
	old := db.Now().Add(-40 * 24 * time.Hour)
	for _, key := range []string{"done-old", "failed-old", "pending"} {
		if _, err := insert(ctx, q, "x", nil, key, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Write.ExecContext(ctx, "UPDATE jobs SET completed_at = run_at WHERE unique_key = 'done-old'"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Write.ExecContext(ctx, "UPDATE jobs SET failed_at = run_at WHERE unique_key = 'failed-old'"); err != nil {
		t.Fatal(err)
	}

	if err := Cleanup(d, discard)(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var left string
	if err := d.Read.QueryRowContext(ctx, "SELECT unique_key FROM jobs").Scan(&left); err != nil {
		t.Fatal(err)
	}
	if n := countJobs(t, d); n != 1 || left != "pending" {
		t.Errorf("after cleanup: %d jobs (first %q), want only the pending one", n, left)
	}
}
