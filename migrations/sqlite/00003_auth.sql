-- +goose Up
-- Sessions and API tokens. Only a SHA-256 hash of each token is stored, so a
-- leaked database can't be used to log in.
CREATE TABLE sessions (
    token_hash      TEXT PRIMARY KEY,
    id              TEXT NOT NULL UNIQUE, -- names the session in the device list
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_agent      TEXT NOT NULL,
    ip              TEXT NOT NULL,        -- the address it was last seen from
    created_at      DATETIME NOT NULL,
    last_seen_at    DATETIME NOT NULL,
    idle_expires_at DATETIME NOT NULL,
    expires_at      DATETIME NOT NULL
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- Single-use tokens sent by email: verification, password reset and email
-- change. email is the address the token was sent to.
CREATE TABLE user_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    TEXT NOT NULL CHECK (purpose IN ('verify_email', 'reset_password', 'change_email')),
    email      TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL
);

CREATE INDEX user_tokens_user_id_idx ON user_tokens (user_id);

-- Invitations to sign up, sent to an address that has no account yet.
-- Signing up through one confirms the address; with REGISTRATION=invite,
-- it's how people get in.
CREATE TABLE signup_invitations (
    token_hash TEXT PRIMARY KEY,
    email      TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL
);

CREATE INDEX signup_invitations_email_idx ON signup_invitations (email);

-- +goose Down
DROP TABLE signup_invitations;
DROP TABLE user_tokens;
DROP TABLE sessions;
