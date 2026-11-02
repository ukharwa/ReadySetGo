-- example:begin
-- +goose Up
CREATE TABLE notes (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

-- A user's notes, most recently changed first.
CREATE INDEX notes_user_id_updated_at_idx ON notes (user_id, updated_at);

-- +goose Down
DROP TABLE notes;
-- example:end
