package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/jobs/store"
)

// CleanupKind is the scheduled job that deletes finished jobs.
const CleanupKind = "jobs.cleanup"

// Completed jobs are kept a week, which is what keeps a scheduled task's
// unique key taken (so tasks can't repeat) for intervals up to 7 days.
// Failed jobs are kept 30 days for inspection.
const (
	keepCompleted = 7 * 24 * time.Hour
	keepFailed    = 30 * 24 * time.Hour
)

// Cleanup returns the handler for CleanupKind.
func Cleanup(d *db.DB, logger *slog.Logger) Handler {
	return func(ctx context.Context, _ []byte) error {
		now := db.Now()
		n, err := store.New(d.Write).DeleteOldJobs(ctx, store.DeleteOldJobsParams{
			CompletedBefore: nullTime(now.Add(-keepCompleted)),
			FailedBefore:    nullTime(now.Add(-keepFailed)),
		})
		if err != nil {
			return fmt.Errorf("delete old jobs: %w", err)
		}
		logger.InfoContext(ctx, "old jobs deleted", "count", n)
		return nil
	}
}
