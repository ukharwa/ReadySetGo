-- name: SearchUsers :many
-- Users whose address matches the LIKE pattern @pattern, by address.
SELECT id, email, role, email_verified_at, disabled_at, created_at
FROM users
WHERE email LIKE CAST(@pattern AS TEXT) ESCAPE '\'
ORDER BY email
LIMIT CAST(@max_rows AS BIGINT) OFFSET CAST(@skip AS BIGINT);

-- name: GetUser :one
SELECT id, email, role, email_verified_at, disabled_at, created_at
FROM users
WHERE id = @id;

-- name: ListFailedJobs :many
-- The newest failed jobs. Their payloads stay hidden: an email's holds its
-- links, tokens included.
SELECT id, kind, attempts, last_error, failed_at, created_at
FROM jobs
WHERE failed_at IS NOT NULL
ORDER BY failed_at DESC
LIMIT 100;

-- name: RetryJob :execrows
-- Gives a failed job a fresh set of attempts, starting now.
UPDATE jobs SET failed_at = NULL, attempts = 0, last_error = NULL, run_at = @run_at
WHERE id = @id AND failed_at IS NOT NULL;
