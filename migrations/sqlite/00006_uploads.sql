-- readysetgo:begin module=uploads
-- +goose Up
-- Uploaded files' details; the files themselves are in storage under
-- storage_key, a random name, never the user's filename.
CREATE TABLE uploads (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    storage_key  TEXT NOT NULL UNIQUE,
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size         INTEGER NOT NULL,
    created_at   DATETIME NOT NULL
);

CREATE INDEX uploads_user_id_idx ON uploads (user_id);

-- +goose Down
DROP TABLE uploads;
-- readysetgo:end
