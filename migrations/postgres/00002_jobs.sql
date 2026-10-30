-- +goose Up
-- payload is JSON stored as text: the queue never queries inside it.
CREATE TABLE jobs (
    id           uuid PRIMARY KEY,
    kind         text NOT NULL,
    payload      text NOT NULL,
    unique_key   text,
    run_at       timestamptz NOT NULL,
    attempts     bigint NOT NULL,
    max_attempts bigint NOT NULL,
    locked_until timestamptz,
    last_error   text,
    completed_at timestamptz,
    failed_at    timestamptz,
    created_at   timestamptz NOT NULL,
    CONSTRAINT jobs_unique_key_key UNIQUE (unique_key)
);

CREATE INDEX jobs_ready_idx ON jobs (run_at) WHERE completed_at IS NULL AND failed_at IS NULL;

-- +goose Down
DROP TABLE jobs;
