-- Every query takes the owner's user_id, so one user can never reach
-- another's notes.

-- name: CreateNote :one
INSERT INTO notes (id, user_id, title, body, created_at, updated_at)
VALUES (@id, @user_id, @title, @body, @created_at, @updated_at)
RETURNING *;

-- name: ListNotes :many
SELECT * FROM notes WHERE user_id = @user_id ORDER BY updated_at DESC, id DESC;

-- name: GetNote :one
SELECT * FROM notes WHERE id = @id AND user_id = @user_id;

-- name: UpdateNote :one
UPDATE notes SET title = @title, body = @body, updated_at = @updated_at
WHERE id = @id AND user_id = @user_id
RETURNING *;

-- name: DeleteNote :execrows
DELETE FROM notes WHERE id = @id AND user_id = @user_id;
