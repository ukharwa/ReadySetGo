-- readysetgo:begin module=twofactor
-- +goose Up
-- Two-factor login: the secret each user's authenticator app shares,
-- encrypted with TWO_FACTOR_KEY, and their unused recovery codes, hashed.
-- last_step is the time step of the latest code used, so none is used twice.
CREATE TABLE two_factor (
    user_id    TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret     BLOB NOT NULL,
    last_step  INTEGER NOT NULL,
    enabled_at DATETIME NOT NULL
);

CREATE TABLE recovery_codes (
    user_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);

-- +goose Down
DROP TABLE recovery_codes;
DROP TABLE two_factor;
-- readysetgo:end
