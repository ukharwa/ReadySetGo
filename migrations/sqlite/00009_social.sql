-- readysetgo:begin module=social
-- +goose Up
-- Accounts at other services (Google, GitHub) that people log in with:
-- subject is the service's own ID for the account, which never changes.
-- Each user links at most one account per service.
CREATE TABLE user_identities (
    provider   TEXT NOT NULL,
    subject    TEXT NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    PRIMARY KEY (provider, subject)
);

CREATE UNIQUE INDEX user_identities_user_id_provider_idx ON user_identities (user_id, provider);

-- +goose Down
DROP TABLE user_identities;
-- readysetgo:end
