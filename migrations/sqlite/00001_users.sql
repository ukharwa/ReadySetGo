-- +goose Up
CREATE TABLE users (
    id                TEXT PRIMARY KEY,
    email             TEXT NOT NULL UNIQUE,
    name              TEXT NOT NULL, -- what the app calls them; may be empty
    time_zone         TEXT NOT NULL, -- an IANA name, such as Africa/Johannesburg
    password_hash     TEXT NOT NULL,
    role              TEXT NOT NULL CHECK (role IN ('user', 'admin')),
    email_verified_at DATETIME,
    disabled_at       DATETIME,
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL
);

-- +goose Down
DROP TABLE users;
