-- Users

-- name: CreateUser :one
INSERT INTO users (id, email, password_hash, role, email_verified_at, created_at, updated_at)
VALUES (@id, @email, @password_hash, @role, @email_verified_at, @created_at, @updated_at)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = @email;

-- name: UpdatePasswordHash :exec
UPDATE users SET password_hash = @password_hash, updated_at = @updated_at WHERE id = @id;

-- name: MarkEmailVerified :execrows
-- Only verifies the address the token was sent to, in case it has changed.
UPDATE users SET email_verified_at = @verified_at, updated_at = @verified_at
WHERE id = @id AND email = @email AND email_verified_at IS NULL;

-- name: UpdateEmail :exec
UPDATE users SET email = @email, email_verified_at = @verified_at, updated_at = @verified_at WHERE id = @id;

-- name: SetRole :execrows
UPDATE users SET role = @role, updated_at = @updated_at WHERE id = @id;

-- name: SetDisabled :execrows
UPDATE users SET disabled_at = @disabled_at, updated_at = @updated_at WHERE id = @id;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = @id;

-- Sessions

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, id, user_id, user_agent, ip, created_at, last_seen_at, idle_expires_at, expires_at)
VALUES (@token_hash, @id, @user_id, @user_agent, @ip, @created_at, @last_seen_at, @idle_expires_at, @expires_at);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = @token_hash;

-- name: ListUserSessions :many
-- A user's sessions that haven't expired, most recently used first.
SELECT * FROM sessions
WHERE user_id = @user_id AND expires_at > @now AND idle_expires_at > @now
ORDER BY last_seen_at DESC, id;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = @last_seen_at, idle_expires_at = @idle_expires_at, ip = @ip
WHERE token_hash = @token_hash;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = @token_hash;

-- name: DeleteUserSession :execrows
DELETE FROM sessions WHERE user_id = @user_id AND id = @id;

-- name: DeleteUserSessions :exec
-- Deletes all of a user's sessions except the one whose hash is @keep.
DELETE FROM sessions WHERE user_id = @user_id AND token_hash <> @keep;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < @now OR idle_expires_at < @now;

-- Emailed tokens

-- name: CreateUserToken :exec
INSERT INTO user_tokens (token_hash, user_id, purpose, email, created_at, expires_at)
VALUES (@token_hash, @user_id, @purpose, @email, @created_at, @expires_at);

-- name: TakeUserToken :one
-- Deletes and returns a token, so it can be used only once.
DELETE FROM user_tokens WHERE token_hash = @token_hash AND purpose = @purpose
RETURNING *;

-- name: DeleteUserTokens :exec
DELETE FROM user_tokens WHERE user_id = @user_id AND purpose = @purpose;

-- name: DeleteExpiredUserTokens :execrows
DELETE FROM user_tokens WHERE expires_at < @now;
