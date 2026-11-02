-- example:begin
-- +goose Up
CREATE TABLE notes (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT notes_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

-- A user's notes, most recently changed first.
CREATE INDEX notes_user_id_updated_at_idx ON notes (user_id, updated_at);

-- +goose Down
DROP TABLE notes;
-- example:end
