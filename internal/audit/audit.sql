-- name: InsertEvent :exec
INSERT INTO audit_events (id, event, user_id, actor_id, detail, ip, request_id, created_at)
VALUES (@id, @event, @user_id, @actor_id, @detail, @ip, @request_id, @created_at);

-- name: ListEvents :many
-- Newest first, with the addresses the user and actor have now. A non-empty
-- @email keeps only events about that account.
SELECT e.id, e.event, e.user_id, e.actor_id, e.detail, e.ip, e.request_id, e.created_at,
       COALESCE(u.email, '') AS user_email, COALESCE(a.email, '') AS actor_email
FROM audit_events e
LEFT JOIN users u ON u.id = e.user_id
LEFT JOIN users a ON a.id = e.actor_id
WHERE CAST(@email AS TEXT) = '' OR u.email = @email
ORDER BY e.created_at DESC, e.id DESC
LIMIT CAST(@max_rows AS BIGINT) OFFSET CAST(@skip AS BIGINT);
