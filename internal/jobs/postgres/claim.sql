-- name: ClaimJob :one
-- Takes the oldest ready job and leases it until locked_until. SKIP LOCKED
-- lets several workers claim different jobs at once without waiting on, or
-- double-claiming, each other's rows.
UPDATE jobs SET attempts = attempts + 1, locked_until = @locked_until
WHERE id = (
    SELECT j.id FROM jobs j
    WHERE j.run_at <= @now
      AND j.completed_at IS NULL AND j.failed_at IS NULL
      AND (j.locked_until IS NULL OR j.locked_until < @now)
    ORDER BY j.run_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;
