-- readysetgo:begin module=audit
-- +goose Up
-- Security events, kept for good. Deleting a user clears the references to
-- them and keeps the events.
CREATE TABLE audit_events (
    id         uuid PRIMARY KEY,
    event      text NOT NULL,
    user_id    uuid,
    actor_id   uuid,
    detail     text NOT NULL,
    ip         text NOT NULL,
    request_id text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT audit_events_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT audit_events_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE SET NULL
);

CREATE INDEX audit_events_user_id_idx ON audit_events (user_id);
CREATE INDEX audit_events_actor_id_idx ON audit_events (actor_id);
CREATE INDEX audit_events_created_at_idx ON audit_events (created_at);

-- +goose Down
DROP TABLE audit_events;
-- readysetgo:end
