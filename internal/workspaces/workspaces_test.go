package workspaces_test

import (
	"errors"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
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
