-- name: CountQueuedJobs :one
-- Jobs waiting to run or running.
SELECT COUNT(*) FROM jobs WHERE completed_at IS NULL AND failed_at IS NULL;

-- name: CountFailedJobs :one
-- Jobs that failed on every attempt and haven't been cleaned up yet.
SELECT COUNT(*) FROM jobs WHERE failed_at IS NOT NULL;
