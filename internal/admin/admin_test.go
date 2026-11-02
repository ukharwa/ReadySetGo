package admin_test

import (
	"net/http"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
)

// signInAdmin signs a client in as a new admin.
func signInAdmin(t *testing.T, a *apptest.App) (*http.Client, auth.User) {
	t.Helper()
	c := a.Client()
	u := a.SignIn(c, "admin@example.com")
	if err := a.Accounts.SetRole(t.Context(), u.ID, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	return c, u
}
