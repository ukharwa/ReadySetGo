package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/jobs/store"
)

// checkInterval is how often the scheduler looks for a task that is due.
const checkInterval = 10 * time.Second

// Scheduler enqueues recurring jobs. Every instance runs one, and each run of
// a task gets a unique key (the task's kind plus its scheduled time), so the
// database lets only the first instance enqueue it: a task runs once per
// interval however many instances there are.
type Scheduler struct {
	db     *db.DB
	logger *slog.Logger
	tasks  []task
	now    func() time.Time // replaced in tests
}

type task struct {
	kind     string
	interval time.Duration
	last     time.Time // last run this instance enqueued or saw taken
}

// NewScheduler returns a scheduler with no tasks.
func NewScheduler(d *db.DB, logger *slog.Logger) *Scheduler {
	return &Scheduler{db: d, logger: logger, now: db.Now}
}

// Every enqueues a job of kind, with an empty payload, once per interval.
// Runs line up with UTC: Every(time.Hour, …) runs on the hour,
// Every(24*time.Hour, …) at midnight UTC. The worker needs a handler for
// kind. Intervals up to 7 days are supported (see DeleteOldJobs). Call it
// before Run.
func (s *Scheduler) Every(interval time.Duration, kind string) {
	s.tasks = append(s.tasks, task{kind: kind, interval: interval})
}

// Run enqueues due tasks until ctx is cancelled. A task that is due when the
// app starts (say, a daily one that hasn't run today) is enqueued at once.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		s.EnqueueDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkInterval):
		}
	}
}

// EnqueueDue enqueues every task whose current run hasn't been enqueued yet.
func (s *Scheduler) EnqueueDue(ctx context.Context) {
	q := store.New(s.db.Write)
	for i := range s.tasks {
		t := &s.tasks[i]
		at := s.now().Truncate(t.interval)
		if at.Equal(t.last) {
			continue
		}
		key := "schedule:" + t.kind + ":" + at.Format(time.RFC3339)
		added, err := insert(ctx, q, t.kind, struct{}{}, key, at)
		if err != nil {
			s.logger.ErrorContext(ctx, "schedule task", "kind", t.kind, "err", err)
			continue
		}
		t.last = at
		if added {
			s.logger.InfoContext(ctx, "scheduled task enqueued", "kind", t.kind, "for", at)
		}
	}
}
