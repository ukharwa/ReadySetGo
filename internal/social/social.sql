-- name: GetIdentity :one
SELECT * FROM user_identities WHERE provider = @provider AND subject = @subject;

-- name: CreateIdentity :exec
INSERT INTO user_identities (provider, subject, user_id, email, created_at)
VALUES (@provider, @subject, @user_id, @email, @created_at);

-- name: ListIdentities :many
SELECT * FROM user_identities WHERE user_id = @user_id ORDER BY provider;

-- name: DeleteIdentity :exec
DELETE FROM user_identities WHERE user_id = @user_id AND provider = @provider;
