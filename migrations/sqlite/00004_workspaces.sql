-- kickstart:begin module=workspaces
-- +goose Up
CREATE TABLE workspaces (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE workspace_members (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at   DATETIME NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX workspace_members_user_id_idx ON workspace_members (user_id);

-- One pending invitation per address per workspace; inviting again replaces
-- it. Only a hash of the emailed token is stored.
CREATE TABLE workspace_invitations (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email        TEXT NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('admin', 'member')),
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   DATETIME NOT NULL,
    expires_at   DATETIME NOT NULL,
    UNIQUE (workspace_id, email)
);

CREATE INDEX workspace_invitations_email_idx ON workspace_invitations (email);

-- +goose Down
DROP TABLE workspace_invitations;
DROP TABLE workspace_members;
DROP TABLE workspaces;
-- kickstart:end
