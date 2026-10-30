-- +goose Up
CREATE TABLE users (
    id                uuid PRIMARY KEY,
    email             text NOT NULL,
    password_hash     text NOT NULL,
    role              text NOT NULL,
    email_verified_at timestamptz,
    disabled_at       timestamptz,
    created_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL,
    CONSTRAINT users_email_key UNIQUE (email),
    CONSTRAINT users_role_check CHECK (role IN ('user', 'admin'))
);

-- +goose Down
DROP TABLE users;
