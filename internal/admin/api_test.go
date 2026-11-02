package admin_test

import (
	"net/http"
	"testing"

	"github.com/ukharwa/readysetgo/internal/admin"
	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
)

type userJSON struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
}

func TestAPIUsers(t *testing.T) {
	t.Parallel()
	a := apptest.New(t)
	admin.NewAPI(admin.NewService(a.DB, a.Accounts, a.Logger)).Routes(a.Mux)
	adminClient, me := signInAdmin(t, a)
	userClient := a.Client()
	bob := a.SignIn(userClient, "bob@example.com")
	path := "/api/admin/users/" + bob.ID.String()

	var users []userJSON
	a.Decode(a.JSON(adminClient, "GET", "/api/admin/users?q=bob", "", nil), &users)
	if len(users) != 1 || users[0].Email != "bob@example.com" {
		t.Errorf("search: %+v", users)
	}

	var updated userJSON
	r := a.JSON(adminClient, "PATCH", path, "", map[string]any{"role": "admin", "disabled": true})
	a.Decode(r, &updated)
	if r.Status != http.StatusOK || updated.Role != auth.RoleAdmin || !updated.Disabled {
		t.Errorf("update: %d %+v", r.Status, updated)
	}
	if r := a.JSON(adminClient, "PATCH", path, "", map[string]any{"role": "owner"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("unknown role: %d, want 422", r.Status)
	}
	if r := a.JSON(adminClient, "PATCH", "/api/admin/users/"+me.ID.String(), "", map[string]any{"disabled": true}); r.Status != http.StatusForbidden {
		t.Errorf("disabling own account: %d, want 403", r.Status)
	}
	if r := a.JSON(adminClient, "DELETE", path, "", nil); r.Status != http.StatusNoContent {
		t.Errorf("delete: %d %s", r.Status, r.Body)
	}
	if r := a.JSON(adminClient, "GET", path, "", nil); r.Status != http.StatusNotFound {
		t.Errorf("deleted user: %d, want 404", r.Status)
	}

	carol := a.Client()
	a.SignIn(carol, "carol@example.com")
	if r := a.JSON(carol, "GET", "/api/admin/users", "", nil); r.Status != http.StatusForbidden {
		t.Errorf("as a user: %d, want 403", r.Status)
	}
	if r := a.JSON(carol, "GET", "/api/admin/failed-jobs", "", nil); r.Status != http.StatusForbidden {
		t.Errorf("failed jobs as a user: %d, want 403", r.Status)
	}
}
