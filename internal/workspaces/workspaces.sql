-- Workspaces and memberships

-- name: CreateWorkspace :exec
INSERT INTO workspaces (id, name, created_at, updated_at) VALUES (@id, @name, @created_at, @created_at);

-- name: RenameWorkspace :exec
UPDATE workspaces SET name = @name, updated_at = @updated_at WHERE id = @id;

-- name: DeleteWorkspace :exec
DELETE FROM workspaces WHERE id = @id;

-- name: AddMember :exec
INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
VALUES (@workspace_id, @user_id, @role, @created_at);

-- name: GetMembership :one
SELECT w.id, w.name, w.created_at, m.role
FROM workspaces w JOIN workspace_members m ON m.workspace_id = w.id
WHERE w.id = @workspace_id AND m.user_id = @user_id;

-- name: ListUserWorkspaces :many
SELECT w.id, w.name, w.created_at, m.role
FROM workspaces w JOIN workspace_members m ON m.workspace_id = w.id
WHERE m.user_id = @user_id
ORDER BY w.name, w.id;

-- name: ListMembers :many
SELECT m.user_id, u.email, m.role, m.created_at
FROM workspace_members m JOIN users u ON u.id = m.user_id
WHERE m.workspace_id = @workspace_id
ORDER BY u.email;

-- name: SetMemberRole :execrows
UPDATE workspace_members SET role = @role WHERE workspace_id = @workspace_id AND user_id = @user_id;

-- name: RemoveMember :execrows
DELETE FROM workspace_members WHERE workspace_id = @workspace_id AND user_id = @user_id;

-- name: CountOwners :one
SELECT COUNT(*) FROM workspace_members WHERE workspace_id = @workspace_id AND role = 'owner';

-- name: SoleOwnerWorkspacesWithOthers :many
-- Workspaces the user alone owns that also have other members: the user
-- must hand them over before deleting their account.
SELECT w.id, w.name
FROM workspaces w JOIN workspace_members m ON m.workspace_id = w.id
WHERE m.user_id = @user_id AND m.role = 'owner'
  AND (SELECT COUNT(*) FROM workspace_members o WHERE o.workspace_id = w.id AND o.role = 'owner') = 1
  AND (SELECT COUNT(*) FROM workspace_members a WHERE a.workspace_id = w.id) > 1
ORDER BY w.name;

-- name: DeleteSoleMemberWorkspaces :exec
-- Deletes the workspaces whose only member is the user.
DELETE FROM workspaces WHERE id IN (
    SELECT m.workspace_id FROM workspace_members m
    WHERE m.user_id = @user_id
      AND (SELECT COUNT(*) FROM workspace_members a WHERE a.workspace_id = m.workspace_id) = 1
);

-- Invitations

-- name: UpsertInvitation :exec
INSERT INTO workspace_invitations (id, workspace_id, email, role, token_hash, created_at, expires_at)
VALUES (@id, @workspace_id, @email, @role, @token_hash, @created_at, @expires_at)
ON CONFLICT (workspace_id, email) DO UPDATE SET
    role = excluded.role, token_hash = excluded.token_hash,
    created_at = excluded.created_at, expires_at = excluded.expires_at;

-- name: ListInvitations :many
SELECT id, email, role, created_at, expires_at
FROM workspace_invitations
WHERE workspace_id = @workspace_id AND expires_at > @now
ORDER BY email;

-- name: GetInvitationByToken :one
SELECT i.id, i.workspace_id, i.email, i.role, i.expires_at, w.name AS workspace_name
FROM workspace_invitations i JOIN workspaces w ON w.id = i.workspace_id
WHERE i.token_hash = @token_hash;

-- name: DeleteInvitation :execrows
DELETE FROM workspace_invitations WHERE id = @id AND workspace_id = @workspace_id;

-- name: CountPendingInvitations :one
SELECT COUNT(*) FROM workspace_invitations WHERE email = @email AND expires_at > @now;

-- name: DeleteExpiredInvitations :execrows
DELETE FROM workspace_invitations WHERE expires_at < @now;
