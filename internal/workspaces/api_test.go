package workspaces_test

import (
	"net/http"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/workspaces"
	"github.com/ukharwa/readysetgo/internal/workspaces/workspacestest"
)

func newAPIApp(t *testing.T) (*apptest.App, *workspaces.Service) {
	t.Helper()
	a, svc := workspacestest.New(t)
	workspaces.NewAPI(svc).Routes(a.Mux)
	return a, svc
}

type workspaceJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func TestAPIWorkspaces(t *testing.T) {
	t.Parallel()
	a, svc := newAPIApp(t)
	alice := workspacestest.SignIn(t, a, svc, "alice@example.com")
	bob := workspacestest.SignIn(t, a, svc, "bob@example.com")
	base := "/api/workspaces/" + alice.Workspace.ID.String()

	var created workspaceJSON
	r := a.JSON(alice.Client, "POST", "/api/workspaces", "", map[string]string{"name": "Acme"})
	a.Decode(r, &created)
	if r.Status != http.StatusCreated || created.Name != "Acme" || created.Role != workspaces.RoleOwner {
		t.Fatalf("create: %d %+v", r.Status, created)
	}
	var mine []workspaceJSON
	a.Decode(a.JSON(alice.Client, "GET", "/api/workspaces", "", nil), &mine)
	if len(mine) != 2 {
		t.Errorf("Alice's workspaces: %+v, want her personal one and Acme", mine)
	}

	// Outsiders get 404 for everything under another's workspace.
	for _, req := range [][2]string{{"GET", base}, {"GET", base + "/members"}, {"PATCH", base}, {"DELETE", base}} {
		if r := a.JSON(bob.Client, req[0], req[1], "", map[string]string{"name": "x"}); r.Status != http.StatusNotFound {
			t.Errorf("%s %s as an outsider: %d, want 404", req[0], req[1], r.Status)
		}
	}

	// Invite Bob, who accepts through the API.
	if r := a.JSON(alice.Client, "POST", base+"/invitations", "", map[string]string{"email": "bob@example.com", "role": "admin"}); r.Status != http.StatusAccepted {
		t.Fatalf("invite: %d %s", r.Status, r.Body)
	}
	token := a.TokenFrom("bob@example.com", "/invitations/accept")
	var joined workspaceJSON
	a.Decode(a.JSON(bob.Client, "POST", "/api/invitations/accept", "", map[string]string{"token": token}), &joined)
	if joined.ID != alice.Workspace.ID.String() || joined.Role != workspaces.RoleAdmin {
		t.Errorf("accept: %+v", joined)
	}
	var members []struct{ Email, Role string }
	a.Decode(a.JSON(bob.Client, "GET", base+"/members", "", nil), &members)
	if len(members) != 2 {
		t.Errorf("members: %+v", members)
	}

	// An admin can't delete the workspace; the owner can.
	if r := a.JSON(bob.Client, "DELETE", base, "", nil); r.Status != http.StatusForbidden {
		t.Errorf("admin deleting: %d, want 403", r.Status)
	}
	if r := a.JSON(alice.Client, "DELETE", base, "", nil); r.Status != http.StatusNoContent {
		t.Errorf("owner deleting: %d %s", r.Status, r.Body)
	}
	if r := a.JSON(bob.Client, "GET", base, "", nil); r.Status != http.StatusNotFound {
		t.Errorf("deleted workspace: %d, want 404", r.Status)
	}
}

func TestAPIValidation(t *testing.T) {
	t.Parallel()
	a, svc := newAPIApp(t)
	alice := workspacestest.SignIn(t, a, svc, "alice@example.com")
	base := "/api/workspaces/" + alice.Workspace.ID.String()
	tests := []struct {
		method, path string
		body         any
		want         int
	}{
		{"POST", "/api/workspaces", map[string]string{"name": ""}, http.StatusUnprocessableEntity},
		{"POST", base + "/invitations", map[string]string{"email": "nope", "role": "owner"}, http.StatusUnprocessableEntity},
		{"POST", base + "/invitations", map[string]string{"email": "alice@example.com", "role": "member"}, http.StatusUnprocessableEntity},
		{"PATCH", base + "/members/" + alice.User.ID.String(), map[string]string{"role": "member"}, http.StatusForbidden}, // last owner
		{"POST", "/api/invitations/accept", map[string]string{"token": "nonsense"}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		if r := a.JSON(alice.Client, tt.method, tt.path, "", tt.body); r.Status != tt.want {
			t.Errorf("%s %s %v: %d %s, want %d", tt.method, tt.path, tt.body, r.Status, r.Body, tt.want)
		}
	}
}
