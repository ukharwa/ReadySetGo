-- +goose Up
CREATE TABLE jobs (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    payload      TEXT NOT NULL,
    unique_key   TEXT UNIQUE,
    run_at       DATETIME NOT NULL,
    attempts     INTEGER NOT NULL,
    max_attempts INTEGER NOT NULL,
    locked_until DATETIME,
    last_error   TEXT,
    completed_at DATETIME,
    failed_at    DATETIME,
    created_at   DATETIME NOT NULL
);

CREATE INDEX jobs_ready_idx ON jobs (run_at) WHERE completed_at IS NULL AND failed_at IS NULL;

-- +goose Down
DROP TABLE jobs;
