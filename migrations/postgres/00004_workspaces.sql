-- readysetgo:begin module=workspaces
-- +goose Up
CREATE TABLE workspaces (
    id         uuid PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE workspace_members (
    workspace_id uuid NOT NULL,
    user_id      uuid NOT NULL,
    role         text NOT NULL,
    created_at   timestamptz NOT NULL,
    CONSTRAINT workspace_members_pkey PRIMARY KEY (workspace_id, user_id),
    CONSTRAINT workspace_members_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT workspace_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT workspace_members_role_check CHECK (role IN ('owner', 'admin', 'member'))
);

CREATE INDEX workspace_members_user_id_idx ON workspace_members (user_id);

-- One pending invitation per address per workspace; inviting again replaces
-- it. Only a hash of the emailed token is stored.
CREATE TABLE workspace_invitations (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    email        text NOT NULL,
    role         text NOT NULL,
    token_hash   text NOT NULL,
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    CONSTRAINT workspace_invitations_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT workspace_invitations_role_check CHECK (role IN ('admin', 'member')),
    CONSTRAINT workspace_invitations_token_hash_key UNIQUE (token_hash),
    CONSTRAINT workspace_invitations_workspace_id_email_key UNIQUE (workspace_id, email)
);

CREATE INDEX workspace_invitations_email_idx ON workspace_invitations (email);

-- +goose Down
DROP TABLE workspace_invitations;
DROP TABLE workspace_members;
DROP TABLE workspaces;
-- readysetgo:end
