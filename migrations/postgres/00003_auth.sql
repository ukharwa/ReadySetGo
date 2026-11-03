-- +goose Up
-- Sessions and API tokens. Only a SHA-256 hash of each token is stored, so a
-- leaked database can't be used to log in.
CREATE TABLE sessions (
    token_hash      text PRIMARY KEY,
    id              uuid NOT NULL UNIQUE, -- names the session in the device list
    user_id         uuid NOT NULL,
    user_agent      text NOT NULL,
    ip              text NOT NULL,        -- the address it was last seen from
    created_at      timestamptz NOT NULL,
    last_seen_at    timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL,
    CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- Single-use tokens sent by email: verification, password reset and email
-- change. email is the address the token was sent to.
CREATE TABLE user_tokens (
    token_hash text PRIMARY KEY,
    user_id    uuid NOT NULL,
    purpose    text NOT NULL,
    email      text NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CONSTRAINT user_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT user_tokens_purpose_check CHECK (purpose IN ('verify_email', 'reset_password', 'change_email'))
);

CREATE INDEX user_tokens_user_id_idx ON user_tokens (user_id);

-- +goose Down
DROP TABLE user_tokens;
DROP TABLE sessions;
