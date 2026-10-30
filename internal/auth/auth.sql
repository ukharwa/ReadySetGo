-- name: CreateUser :one
INSERT INTO users (id, email, password_hash, role, created_at, updated_at)
VALUES (@id, @email, @password_hash, @role, @created_at, @updated_at)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = @email;
