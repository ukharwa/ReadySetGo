-- readysetgo:begin module=twofactor
-- +goose Up
-- Two-factor login: the secret each user's authenticator app shares,
-- encrypted with TWO_FACTOR_KEY, and their unused recovery codes, hashed.
-- last_step is the time step of the latest code used, so none is used twice.
CREATE TABLE two_factor (
    user_id    uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret     bytea NOT NULL,
    last_step  bigint NOT NULL,
    enabled_at timestamptz NOT NULL
);

CREATE TABLE recovery_codes (
    user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash text NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);

-- +goose Down
DROP TABLE recovery_codes;
DROP TABLE two_factor;
-- readysetgo:end
