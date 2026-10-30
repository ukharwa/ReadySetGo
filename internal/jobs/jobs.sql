-- name: InsertJob :execrows
-- A job whose unique_key already exists is skipped (0 rows).
INSERT INTO jobs (id, kind, payload, unique_key, run_at, attempts, max_attempts, created_at)
VALUES (@id, @kind, @payload, @unique_key, @run_at, 0, @max_attempts, @created_at)
ON CONFLICT (unique_key) DO NOTHING;

-- name: DeleteJob :exec
DELETE FROM jobs WHERE id = @id;

-- name: CompleteJob :exec
-- Jobs with a unique key are kept, marked complete, so the key can't be
-- enqueued again until DeleteOldJobs removes them.
UPDATE jobs SET completed_at = @completed_at, locked_until = NULL WHERE id = @id;

-- name: RetryJob :exec
UPDATE jobs SET run_at = @run_at, locked_until = NULL, last_error = @last_error WHERE id = @id;

-- name: FailJob :exec
UPDATE jobs SET failed_at = @failed_at, locked_until = NULL, last_error = @last_error WHERE id = @id;

-- name: DeleteOldJobs :execrows
DELETE FROM jobs WHERE completed_at < @completed_before OR failed_at < @failed_before;
