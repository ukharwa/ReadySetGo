-- readysetgo:begin module=uploads
-- +goose Up
-- Uploaded files' details; the files themselves are in storage under
-- storage_key, a random name, never the user's filename.
CREATE TABLE uploads (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL,
    storage_key  text NOT NULL,
    filename     text NOT NULL,
    content_type text NOT NULL,
    size         bigint NOT NULL,
    created_at   timestamptz NOT NULL,
    CONSTRAINT uploads_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT uploads_storage_key_key UNIQUE (storage_key)
);

CREATE INDEX uploads_user_id_idx ON uploads (user_id);

-- +goose Down
DROP TABLE uploads;
-- readysetgo:end
