// Package workspaces lets people share data: a workspace has members, each
// an owner, admin or member, who invite others by email.
//
// Everyone has at least one workspace: signing up creates a personal one
// (unless the person was invited to one), so a solo user's data lives in a
// workspace of one and inviting a colleague later moves nothing.
//
// Features whose data belongs to a workspace put their routes under
// /w/{workspaceID}/ (pages) or /api/workspaces/{workspaceID}/ (JSON), guard
// them with Service.Require, and scope every query by From(ctx).ID. See
// AGENTS.md.
package workspaces

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/email"
	"github.com/ukharwa/readysetgo/internal/jobs"
	"github.com/ukharwa/readysetgo/internal/ratelimit"
	"github.com/ukharwa/readysetgo/internal/validate"
	"github.com/ukharwa/readysetgo/internal/workspaces/store"
)

//go:embed emails/*.txt
var emailFS embed.FS

// Roles within a workspace. Owners can do everything, including deleting
// the workspace and changing roles; admins manage members and invitations;
// members use the workspace's data.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// invitationTTL is how long an invitation link works.
const invitationTTL = 7 * 24 * time.Hour

// Errors returned by Service. Their messages are safe to show users.
var (
	ErrNotFound          = errors.New("no such workspace")
	ErrForbidden         = errors.New("your role in this workspace doesn't allow that")
	ErrInvalidInvitation = errors.New("this invitation is invalid or has expired")
	ErrLastOwner         = errors.New("a workspace needs at least one owner: make someone else an owner first")
)

// WrongAddressError reports an invitation opened while logged in with a
// different email address than the one it was sent to.
type WrongAddressError struct {
	InvitedEmail string
}

func (e *WrongAddressError) Error() string {
	return "this invitation was sent to " + e.InvitedEmail + ": log in with that address to accept it"
}

// Workspace is a workspace as the current user sees it: with their role.
type Workspace struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
	Role      string
}

// CanManage reports whether the role may manage members and invitations.
func (w Workspace) CanManage() bool { return w.Role == RoleOwner || w.Role == RoleAdmin }

// IsOwner reports whether the role is owner.
func (w Workspace) IsOwner() bool { return w.Role == RoleOwner }

// Member is someone in a workspace.
type Member struct {
	UserID   uuid.UUID
	Email    string
	Role     string
	JoinedAt time.Time
}

// Invitation is a pending invitation.
type Invitation struct {
	ID        uuid.UUID
	Email     string
	Role      string
	ExpiresAt time.Time
}

// PendingInvitation is an invitation as its recipient sees it.
type PendingInvitation struct {
	WorkspaceName string
	Email         string
	Role          string
}

// Options configures a Service.
type Options struct {
	AppName string
	BaseURL string // invitation links start with this
}

// Service implements workspaces, memberships and invitations.
type Service struct {
	db       *db.DB
	accounts *auth.Service
	limiter  ratelimit.Limiter
	logger   *slog.Logger
	opts     Options
	emails   *email.Templates
	now      func() time.Time // replaced in tests
}

// NewService returns a Service and hooks it into accounts, so new users get
// a personal workspace and deleting an account takes its workspaces into
// account.
func NewService(d *db.DB, accounts *auth.Service, limiter ratelimit.Limiter, logger *slog.Logger, opts Options) (*Service, error) {
	emails, err := email.ParseTemplates(emailFS, "emails/*.txt")
	if err != nil {
		return nil, err
	}
	s := &Service{db: d, accounts: accounts, limiter: limiter, logger: logger, opts: opts, emails: emails, now: db.Now}
	accounts.OnUserCreated(s.createPersonal)
	accounts.OnUserDeleting(s.beforeUserDeleted)
	return s, nil
}

// createPersonal gives a new user a workspace of their own, unless an
// invitation to someone else's is waiting for them.
func (s *Service) createPersonal(ctx context.Context, tx *sql.Tx, u auth.User) error {
	q := store.New(tx)
	pending, err := q.CountPendingInvitations(ctx, store.CountPendingInvitationsParams{Email: u.Email, Now: s.now()})
	if err != nil || pending > 0 {
		return err
	}
	name, _, _ := strings.Cut(u.Email, "@")
	_, err = s.create(ctx, q, u.ID, name+"'s workspace")
	return err
}

// beforeUserDeleted refuses to delete an account that alone owns a
// workspace other people use, and deletes the workspaces only it uses.
func (s *Service) beforeUserDeleted(ctx context.Context, tx *sql.Tx, u auth.User) error {
	q := store.New(tx)
	owned, err := q.SoleOwnerWorkspacesWithOthers(ctx, u.ID)
	if err != nil {
		return err
	}
	if len(owned) > 0 {
		return validate.Errors{"account": fmt.Sprintf(
			"This account is the only owner of “%s”, which other people use. Make one of them an owner, or delete the workspace, first.",
			owned[0].Name)}
	}
	return q.DeleteSoleMemberWorkspaces(ctx, u.ID)
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !validate.Length(name, 1, 100) {
		return "", validate.Errors{"name": "Enter a name of up to 100 characters."}
	}
	return name, nil
}

func (s *Service) create(ctx context.Context, q *store.Queries, owner uuid.UUID, name string) (Workspace, error) {
	now := s.now()
	w := Workspace{ID: db.NewID(), Name: name, CreatedAt: now, Role: RoleOwner}
	if err := q.CreateWorkspace(ctx, store.CreateWorkspaceParams{ID: w.ID, Name: name, CreatedAt: now}); err != nil {
		return Workspace{}, err
	}
	err := q.AddMember(ctx, store.AddMemberParams{WorkspaceID: w.ID, UserID: owner, Role: RoleOwner, CreatedAt: now})
	return w, err
}

// Create makes a workspace with user as its owner.
func (s *Service) Create(ctx context.Context, user auth.User, name string) (Workspace, error) {
	name, err := checkName(name)
	if err != nil {
		return Workspace{}, err
	}
	var w Workspace
	err = s.db.InTx(ctx, func(tx *sql.Tx) error {
		w, err = s.create(ctx, store.New(tx), user.ID, name)
		return err
	})
	return w, err
}

// ForUser lists the workspaces user belongs to.
func (s *Service) ForUser(ctx context.Context, user auth.User) ([]Workspace, error) {
	rows, err := store.New(s.db.Read).ListUserWorkspaces(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Workspace, len(rows))
	for i, r := range rows {
		out[i] = Workspace{ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt, Role: r.Role}
	}
	return out, nil
}

// Membership returns workspace id as user sees it, or ErrNotFound if they
// aren't a member, whether or not it exists.
func (s *Service) Membership(ctx context.Context, id uuid.UUID, user auth.User) (Workspace, error) {
	r, err := store.New(s.db.Read).GetMembership(ctx, store.GetMembershipParams{WorkspaceID: id, UserID: user.ID})
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return Workspace{}, ErrNotFound
	} else if err != nil {
		return Workspace{}, err
	}
	return Workspace{ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt, Role: r.Role}, nil
}

// Rename renames w. Owners and admins only.
func (s *Service) Rename(ctx context.Context, w Workspace, name string) error {
	if !w.CanManage() {
		return ErrForbidden
	}
	name, err := checkName(name)
	if err != nil {
		return err
	}
	return store.New(s.db.Write).RenameWorkspace(ctx, store.RenameWorkspaceParams{ID: w.ID, Name: name, UpdatedAt: s.now()})
}

// Delete deletes w and, through the database's cascades, everything that
// belongs to it. Owners only.
func (s *Service) Delete(ctx context.Context, w Workspace) error {
	if !w.IsOwner() {
		return ErrForbidden
	}
	return store.New(s.db.Write).DeleteWorkspace(ctx, w.ID)
}

// Members lists w's members.
func (s *Service) Members(ctx context.Context, w Workspace) ([]Member, error) {
	rows, err := store.New(s.db.Read).ListMembers(ctx, w.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, len(rows))
	for i, r := range rows {
		out[i] = Member{UserID: r.UserID, Email: r.Email, Role: r.Role, JoinedAt: r.CreatedAt}
	}
	return out, nil
}

// SetRole changes a member's role. Owners only; the last owner can't step
// down.
func (s *Service) SetRole(ctx context.Context, w Workspace, member uuid.UUID, role string) error {
	if !w.IsOwner() {
		return ErrForbidden
	}
	if !slices.Contains([]string{RoleOwner, RoleAdmin, RoleMember}, role) {
		return validate.Errors{"role": "Choose owner, admin or member."}
	}
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		n, err := q.SetMemberRole(ctx, store.SetMemberRoleParams{WorkspaceID: w.ID, UserID: member, Role: role})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return checkOwners(ctx, q, w.ID)
	})
}

// RemoveMember removes member from w. Anyone may remove themselves
// (leave); owners and admins may remove others, but only owners may remove
// an owner. The last owner can't leave.
func (s *Service) RemoveMember(ctx context.Context, w Workspace, actor auth.User, member uuid.UUID) error {
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		if member != actor.ID {
			target, err := q.GetMembership(ctx, store.GetMembershipParams{WorkspaceID: w.ID, UserID: member})
			switch {
			case errors.Is(db.Err(err), db.ErrNotFound):
				return ErrNotFound
			case err != nil:
				return err
			case !w.CanManage() || (target.Role == RoleOwner && !w.IsOwner()):
				return ErrForbidden
			}
		}
		if _, err := q.RemoveMember(ctx, store.RemoveMemberParams{WorkspaceID: w.ID, UserID: member}); err != nil {
			return err
		}
		return checkOwners(ctx, q, w.ID)
	})
}

// checkOwners returns ErrLastOwner if a change left w without an owner,
// which rolls the change back.
func checkOwners(ctx context.Context, q *store.Queries, w uuid.UUID) error {
	n, err := q.CountOwners(ctx, w)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLastOwner
	}
	return nil
}

// Invite emails address a link to join w with role (admin or member).
// Inviting the same address again replaces the earlier link. Owners and
// admins only.
func (s *Service) Invite(ctx context.Context, w Workspace, inviter auth.User, address, role string) error {
	if !w.CanManage() {
		return ErrForbidden
	}
	address, ok := validate.Email(address)
	errs := validate.Errors{}
	if !ok {
		errs["email"] = "Enter a valid email address."
	}
	if role != RoleAdmin && role != RoleMember {
		errs["role"] = "Choose admin or member."
	}
	if len(errs) > 0 {
		return errs
	}
	members, err := s.Members(ctx, w)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(members, func(m Member) bool { return m.Email == address }) {
		return validate.Errors{"email": "That person is already a member."}
	}
	// Invitations are emails too: the same per-address limit as account
	// emails stops the feature being used to flood someone's inbox.
	if ok, _, err := s.limiter.Allow(ctx, "email:"+address, 3, time.Hour); err != nil {
		return err
	} else if !ok {
		return validate.Errors{"email": "Too many emails have gone to that address recently. Try again later."}
	}

	token := rand.Text()
	now := s.now()
	link := s.opts.BaseURL + "/invitations/accept?" + url.Values{"token": {token}}.Encode()
	m, err := s.emails.Message("invitation.txt", address, map[string]any{
		"AppName": s.opts.AppName, "Workspace": w.Name, "Inviter": inviter.Email, "Link": link,
	})
	if err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		err := store.New(tx).UpsertInvitation(ctx, store.UpsertInvitationParams{
			ID: db.NewID(), WorkspaceID: w.ID, Email: address, Role: role,
			TokenHash: hashToken(token), CreatedAt: now, ExpiresAt: now.Add(invitationTTL),
		})
		if err != nil {
			return err
		}
		return email.Enqueue(ctx, tx, m)
	})
}

// Invitations lists w's pending invitations. Owners and admins only.
func (s *Service) Invitations(ctx context.Context, w Workspace) ([]Invitation, error) {
	if !w.CanManage() {
		return nil, ErrForbidden
	}
	rows, err := store.New(s.db.Read).ListInvitations(ctx, store.ListInvitationsParams{WorkspaceID: w.ID, Now: s.now()})
	if err != nil {
		return nil, err
	}
	out := make([]Invitation, len(rows))
	for i, r := range rows {
		out[i] = Invitation{ID: r.ID, Email: r.Email, Role: r.Role, ExpiresAt: r.ExpiresAt}
	}
	return out, nil
}

// RevokeInvitation cancels one of w's invitations. Owners and admins only.
func (s *Service) RevokeInvitation(ctx context.Context, w Workspace, id uuid.UUID) error {
	if !w.CanManage() {
		return ErrForbidden
	}
	n, err := store.New(s.db.Write).DeleteInvitation(ctx, store.DeleteInvitationParams{ID: id, WorkspaceID: w.ID})
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// LookupInvitation returns the invitation a token belongs to, for its
// recipient to look at before accepting.
func (s *Service) LookupInvitation(ctx context.Context, token string) (PendingInvitation, error) {
	r, err := store.New(s.db.Read).GetInvitationByToken(ctx, hashToken(token))
	if errors.Is(db.Err(err), db.ErrNotFound) || (err == nil && !s.now().Before(r.ExpiresAt)) {
		return PendingInvitation{}, ErrInvalidInvitation
	} else if err != nil {
		return PendingInvitation{}, err
	}
	return PendingInvitation{WorkspaceName: r.WorkspaceName, Email: r.Email, Role: r.Role}, nil
}

// Accept adds user to the workspace an invitation is for. The user must be
// logged in with the address it was sent to; since the link came to that
// inbox, the address counts as confirmed.
func (s *Service) Accept(ctx context.Context, user auth.User, token string) (Workspace, error) {
	var w Workspace
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		inv, err := q.GetInvitationByToken(ctx, hashToken(token))
		if errors.Is(db.Err(err), db.ErrNotFound) || (err == nil && !s.now().Before(inv.ExpiresAt)) {
			return ErrInvalidInvitation
		} else if err != nil {
			return err
		}
		if inv.Email != user.Email {
			return &WrongAddressError{InvitedEmail: inv.Email}
		}
		if _, err := q.DeleteInvitation(ctx, store.DeleteInvitationParams{ID: inv.ID, WorkspaceID: inv.WorkspaceID}); err != nil {
			return err
		}
		// readysetgo:begin db=postgres
		// Checked rather than tried: a failed insert would abort the whole
		// transaction.
		// readysetgo:end
		_, err = q.GetMembership(ctx, store.GetMembershipParams{WorkspaceID: inv.WorkspaceID, UserID: user.ID})
		if errors.Is(db.Err(err), db.ErrNotFound) {
			err = q.AddMember(ctx, store.AddMemberParams{WorkspaceID: inv.WorkspaceID, UserID: user.ID, Role: inv.Role, CreatedAt: s.now()})
		}
		if err != nil {
			return err
		}
		w = Workspace{ID: inv.WorkspaceID, Name: inv.WorkspaceName, Role: inv.Role}
		return s.accounts.MarkEmailVerified(ctx, tx, user.ID, user.Email)
	})
	return w, err
}

// CleanupKind is the scheduled job that deletes expired invitations.
const CleanupKind = "workspaces.cleanup"

// Cleanup returns the handler for CleanupKind.
func (s *Service) Cleanup() jobs.Handler {
	return func(ctx context.Context, _ []byte) error {
		n, err := store.New(s.db.Write).DeleteExpiredInvitations(ctx, s.now())
		if err != nil {
			return fmt.Errorf("delete expired invitations: %w", err)
		}
		s.logger.InfoContext(ctx, "expired invitations deleted", "count", n)
		return nil
	}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
