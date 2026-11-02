-- readysetgo:begin module=audit
-- +goose Up
-- Security events, kept for good. Deleting a user clears the references to
-- them and keeps the events.
CREATE TABLE audit_events (
    id         TEXT PRIMARY KEY,
    event      TEXT NOT NULL,
    user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    actor_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    detail     TEXT NOT NULL,
    ip         TEXT NOT NULL,
    request_id TEXT NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE INDEX audit_events_user_id_idx ON audit_events (user_id);
CREATE INDEX audit_events_actor_id_idx ON audit_events (actor_id);
CREATE INDEX audit_events_created_at_idx ON audit_events (created_at);

-- +goose Down
DROP TABLE audit_events;
-- readysetgo:end
