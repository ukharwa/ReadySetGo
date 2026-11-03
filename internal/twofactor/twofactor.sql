-- name: GetTwoFactor :one
SELECT * FROM two_factor WHERE user_id = @user_id;

-- name: CreateTwoFactor :exec
INSERT INTO two_factor (user_id, secret, last_step, enabled_at)
VALUES (@user_id, @secret, @last_step, @enabled_at);

-- name: UseStep :execrows
-- Records the time step of a code just used, unless that step or a later
-- one already was, so each code works once.
UPDATE two_factor SET last_step = @last_step
WHERE user_id = @user_id AND last_step < @last_step;

-- name: DeleteTwoFactor :execrows
DELETE FROM two_factor WHERE user_id = @user_id;

-- name: CreateRecoveryCode :exec
INSERT INTO recovery_codes (user_id, code_hash) VALUES (@user_id, @code_hash);

-- name: TakeRecoveryCode :execrows
DELETE FROM recovery_codes WHERE user_id = @user_id AND code_hash = @code_hash;

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = @user_id;

-- name: CountRecoveryCodes :one
SELECT COUNT(*) FROM recovery_codes WHERE user_id = @user_id;

-- name: GetUser :one
SELECT email, name FROM users WHERE id = @id;
