-- name: CreateUpload :one
INSERT INTO uploads (id, user_id, storage_key, filename, content_type, size, created_at)
VALUES (@id, @user_id, @storage_key, @filename, @content_type, @size, @created_at)
RETURNING *;

-- name: ListUserUploads :many
SELECT * FROM uploads WHERE user_id = @user_id ORDER BY created_at DESC, id DESC;

-- name: GetUserUpload :one
SELECT * FROM uploads WHERE id = @id AND user_id = @user_id;

-- name: DeleteUserUpload :one
DELETE FROM uploads WHERE id = @id AND user_id = @user_id
RETURNING storage_key;

-- name: ListUserStorageKeys :many
SELECT storage_key FROM uploads WHERE user_id = @user_id;
