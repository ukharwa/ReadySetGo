package workspaces_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/workspaces"
	"github.com/ukharwa/readysetgo/internal/workspaces/workspacestest"
)

func newWebApp(t *testing.T) (*apptest.App, *workspaces.Service) {
	t.Helper()
	a, svc := workspacestest.New(t)
	rd := a.Renderer()
	accountPages, err := auth.NewWeb(a.Accounts, rd)
	if err != nil {
		t.Fatal(err)
	}
	accountPages.Routes(a.Mux)
	pages, err := workspaces.NewWeb(svc, rd)
	if err != nil {
		t.Fatal(err)
	}
	pages.Routes(a.Mux)
	return a, svc
}

func wsPath(p workspacestest.Person, rest string) string {
	return "/w/" + p.Workspace.ID.String() + rest
}

func TestWebPersonalWorkspace(t *testing.T) {
	t.Parallel()
	a, svc := newWebApp(t)
	ada := workspacestest.SignIn(t, a, svc, "ada@example.com")
	if p := a.Get(ada.Client, "/workspaces"); !strings.Contains(p.Body, "ada&#39;s workspace") {
		t.Errorf("workspace list:\n%s", p.Body)
	}
	if p := a.Get(ada.Client, wsPath(ada, "")); p.Status != http.StatusOK || !apptest.Contains(p.Body, "ada@example.com", "Invite someone") {
		t.Errorf("workspace page: %d\n%s", p.Status, p.Body)
	}
}

// TestWebIsolation checks that someone outside a workspace gets 404 for all
// of its pages and actions, as if it didn't exist.
func TestWebIsolation(t *testing.T) {
	t.Parallel()
	a, svc := newWebApp(t)
	alice := workspacestest.SignIn(t, a, svc, "alice@example.com")
	bob := workspacestest.SignIn(t, a, svc, "bob@example.com")

	if p := a.Get(bob.Client, wsPath(alice, "")); p.Status != http.StatusNotFound {
		t.Errorf("GET another's workspace: %d, want 404", p.Status)
	}
	for _, path := range []string{"/name", "/invitations", "/delete", "/members/" + alice.User.ID.String() + "/remove"} {
		if p := a.Post(bob.Client, wsPath(alice, path), url.Values{"name": {"mine now"}, "email": {"x@example.com"}, "role": {"member"}}); p.Status != http.StatusNotFound {
			t.Errorf("POST %s on another's workspace: %d, want 404", path, p.Status)
		}
	}
	if p := a.Get(bob.Client, "/w/not-a-uuid"); p.Status != http.StatusNotFound {
		t.Errorf("malformed workspace ID: %d, want 404", p.Status)
	}
}

func TestWebInviteNewcomer(t *testing.T) {
	t.Parallel()
	a, svc := newWebApp(t)
	alice := workspacestest.SignIn(t, a, svc, "alice@example.com")

	if p := a.Post(alice.Client, wsPath(alice, "/invitations"), url.Values{"email": {"Bob@Example.com"}, "role": {"member"}}); p.Status != http.StatusSeeOther {
		t.Fatalf("invite: %d\n%s", p.Status, p.Body)
	}
	token := a.TokenFrom("bob@example.com", "/invitations/accept")

	// Bob has no account: the invitation page sends him to log in, and from
	// there to sign up, keeping the way back.
	bob := a.Client()
	accept := "/invitations/accept?" + url.Values{"token": {token}}.Encode()
	if p := a.Get(bob, accept); p.Status != http.StatusSeeOther || !strings.HasPrefix(p.Location, "/login?next=") {
		t.Fatalf("logged-out invitation: %d → %q", p.Status, p.Location)
	}
	wantLogin := "/login?" + url.Values{"next": {accept}}.Encode()
	if p := a.Post(bob, "/register", url.Values{"email": {"bob@example.com"}, "password": {apptest.Password}, "next": {accept}}); p.Location != wantLogin {
		t.Fatalf("register redirected to %q, want %q", p.Location, wantLogin)
	}
	if p := a.Post(bob, "/login", url.Values{"email": {"bob@example.com"}, "password": {apptest.Password}, "next": {accept}}); p.Location != accept {
		t.Fatalf("login redirected to %q", p.Location)
	}
	if p := a.Get(bob, accept); !apptest.Contains(p.Body, "alice&#39;s workspace", "Join") {
		t.Fatalf("invitation page:\n%s", p.Body)
	}
	if p := a.Post(bob, "/invitations/accept", url.Values{"token": {token}}); p.Status != http.StatusSeeOther || p.Location != wsPath(alice, "") {
		t.Fatalf("accept: %d → %q\n%s", p.Status, p.Location, p.Body)
	}

	// Invited, Bob got no personal workspace of his own.
	if p := a.Get(bob, "/workspaces"); strings.Contains(p.Body, "bob&#39;s workspace") || !strings.Contains(p.Body, "alice&#39;s workspace") {
		t.Errorf("Bob's workspaces:\n%s", p.Body)
	}
	if p := a.Get(alice.Client, wsPath(alice, "")); !strings.Contains(p.Body, "bob@example.com") {
		t.Error("Bob isn't listed as a member")
	}
	if p := a.Post(bob, "/invitations/accept", url.Values{"token": {token}}); p.Status != http.StatusBadRequest {
		t.Errorf("reusing the invitation: %d, want 400", p.Status)
	}
}

func TestWebInvitationForSomeoneElse(t *testing.T) {
	t.Parallel()
	a, svc := newWebApp(t)
	alice := workspacestest.SignIn(t, a, svc, "alice@example.com")
	eve := workspacestest.SignIn(t, a, svc, "eve@example.com")
	a.Post(alice.Client, wsPath(alice, "/invitations"), url.Values{"email": {"carol@example.com"}, "role": {"admin"}})
	token := a.TokenFrom("carol@example.com", "/invitations/accept")

	p := a.Post(eve.Client, "/invitations/accept", url.Values{"token": {token}})
	if p.Status != http.StatusForbidden || !strings.Contains(p.Body, "carol@example.com") {
		t.Errorf("accepting someone else's invitation: %d\n%s", p.Status, p.Body)
	}
}

func TestWebRoles(t *testing.T) {
	t.Parallel()
	a, svc := newWebApp(t)
	alice := workspacestest.SignIn(t, a, svc, "alice@example.com")
	bob := workspacestest.SignIn(t, a, svc, "bob@example.com")
	a.Post(alice.Client, wsPath(alice, "/invitations"), url.Values{"email": {"bob@example.com"}, "role": {"member"}})
	a.Post(bob.Client, "/invitations/accept", url.Values{"token": {a.TokenFrom("bob@example.com", "/invitations/accept")}})

	// A member can't manage the workspace.
	if p := a.Post(bob.Client, wsPath(alice, "/invitations"), url.Values{"email": {"x@example.com"}, "role": {"member"}}); p.Status != http.StatusForbidden {
		t.Errorf("member inviting: %d, want 403", p.Status)
	}
	// The only owner can't leave or step down.
	if p := a.Post(alice.Client, wsPath(alice, "/members/"+alice.User.ID.String()+"/remove"), nil); p.Status != http.StatusForbidden || !strings.Contains(p.Body, "at least one owner") {
		t.Errorf("last owner leaving: %d", p.Status)
	}
	// Once Bob is an owner too, Alice can leave.
	if p := a.Post(alice.Client, wsPath(alice, "/members/"+bob.User.ID.String()+"/role"), url.Values{"role": {"owner"}}); p.Status != http.StatusSeeOther {
		t.Fatalf("promote: %d\n%s", p.Status, p.Body)
	}
	if p := a.Post(alice.Client, wsPath(alice, "/members/"+alice.User.ID.String()+"/remove"), nil); p.Status != http.StatusSeeOther {
		t.Fatalf("leave: %d\n%s", p.Status, p.Body)
	}
	if p := a.Get(alice.Client, wsPath(alice, "")); p.Status != http.StatusNotFound {
		t.Errorf("after leaving: %d, want 404", p.Status)
	}
}

// TestWebDeleteAccount checks the account-deletion hook: an account can't be
// deleted while it alone owns a workspace others use, and its solo
// workspaces go with it.
func TestWebDeleteAccount(t *testing.T) {
	t.Parallel()
	a, svc := newWebApp(t)
	alice := workspacestest.SignIn(t, a, svc, "alice@example.com")
	bob := workspacestest.SignIn(t, a, svc, "bob@example.com")
	a.Post(alice.Client, wsPath(alice, "/invitations"), url.Values{"email": {"bob@example.com"}, "role": {"member"}})
	a.Post(bob.Client, "/invitations/accept", url.Values{"token": {a.TokenFrom("bob@example.com", "/invitations/accept")}})

	p := a.Post(alice.Client, "/account/delete", url.Values{"password": {apptest.Password}})
	if p.Status != http.StatusUnprocessableEntity || !strings.Contains(p.Body, "only owner") {
		t.Fatalf("deleting the only owner: %d\n%s", p.Status, p.Body)
	}

	// Bob alone uses his personal workspace; deleting him deletes it.
	if p := a.Post(bob.Client, "/account/delete", url.Values{"password": {apptest.Password}}); p.Status != http.StatusSeeOther {
		t.Fatalf("delete Bob: %d\n%s", p.Status, p.Body)
	}
	var n int
	if err := a.DB.Read.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM workspaces").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d workspaces left, want only Alice's: Bob's personal one should go with him", n)
	}
	if p := a.Get(alice.Client, wsPath(alice, "")); strings.Contains(p.Body, "bob@example.com") {
		t.Error("Bob is still listed as a member")
	}
}
