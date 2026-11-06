package workspaces_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/validate"
	"github.com/ukharwa/readysetgo/internal/workspaces"
	"github.com/ukharwa/readysetgo/internal/workspaces/workspacestest"
)

// TestInvitationAllowsSignup checks that, when signing up needs an
// invitation, one to join a workspace counts.
func TestInvitationAllowsSignup(t *testing.T) {
	t.Parallel()
	a, svc := workspacestest.New(t, func(o *auth.Options) { o.InviteOnly = true })
	ada := workspacestest.SignIn(t, a, svc, "ada@example.com")
	if err := svc.Invite(t.Context(), ada.Workspace, ada.User, "bob@example.com", workspaces.RoleMember); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"bob@example.com", "carol@example.com"} {
		if err := a.Accounts.Register(t.Context(), auth.Registration{Email: address, Password: apptest.Password}); err != nil {
			t.Fatalf("register %s: %v", address, err)
		}
	}
	if _, err := a.Accounts.UserByEmail(t.Context(), "bob@example.com"); err != nil {
		t.Errorf("bob, invited to a workspace, couldn't sign up: %v", err)
	}
	if _, err := a.Accounts.UserByEmail(t.Context(), "carol@example.com"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("carol, never invited, signed up: err = %v", err)
	}
}

// TestUnconfirmedCantInvite checks that someone whose address isn't
// confirmed can't invite anyone. The email names them by an address they
// may not own, and whoever they let in would stay once the address's owner
// took the account over with a password reset.
func TestUnconfirmedCantInvite(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	a, svc := workspacestest.New(t)
	u, err := a.Accounts.CreateUser(ctx, "ada@example.com", apptest.Password, auth.RoleUser, false)
	if err != nil {
		t.Fatal(err)
	}
	all, err := svc.ForUser(ctx, u)
	if err != nil || len(all) != 1 {
		t.Fatalf("workspaces = %v, %v", all, err)
	}
	err = svc.Invite(ctx, all[0], u, "bob@example.com", workspaces.RoleMember)
	if errs, ok := errors.AsType[validate.Errors](err); !ok || errs["email"] == "" {
		t.Errorf("err = %v, want a message for email", err)
	}
	if sent := a.SentTo("bob@example.com"); len(sent) != 0 {
		t.Errorf("%d invitations sent", len(sent))
	}
}

// TestPersonalWorkspaceName checks that a new person's workspace is named
// after them, by their address when they gave no name or one too long.
func TestPersonalWorkspaceName(t *testing.T) {
	t.Parallel()
	a, svc := workspacestest.New(t)
	tests := []struct{ address, name, want string }{
		{"sam@example.com", "Sam Smoke", "Sam Smoke's workspace"},
		{"dan@example.com", "", "dan's workspace"},
		{"lee@example.com", strings.Repeat("L", 95), "lee's workspace"},
	}
	for _, tt := range tests {
		reg := auth.Registration{Email: tt.address, Password: apptest.Password, Name: tt.name}
		if err := a.Accounts.Register(t.Context(), reg); err != nil {
			t.Fatalf("register %s: %v", tt.address, err)
		}
		u, err := a.Accounts.UserByEmail(t.Context(), tt.address)
		if err != nil {
			t.Fatal(err)
		}
		mine, err := svc.ForUser(t.Context(), u)
		if err != nil || len(mine) != 1 || mine[0].Name != tt.want {
			t.Errorf("%s's workspaces: %+v (err %v), want one named %q", tt.address, mine, err, tt.want)
		}
	}
}
