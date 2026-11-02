package audit_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ukharwa/kickstart/internal/apptest"
	"github.com/ukharwa/kickstart/internal/audit"
	"github.com/ukharwa/kickstart/internal/auth"
)

func newWebApp(t *testing.T) *apptest.App {
	t.Helper()
	a := apptest.New(t)
	svc := audit.NewService(a.DB, a.Accounts, a.Logger)
	rd := a.Renderer()
	accountPages, err := auth.NewWeb(a.Accounts, rd, a.Limiter)
	if err != nil {
		t.Fatal(err)
	}
	accountPages.Routes(a.Mux)
	pages, err := audit.NewWeb(svc, rd)
	if err != nil {
		t.Fatal(err)
	}
	pages.Routes(a.Mux)
	return a
}

// signInAdmin signs a client in as a new admin.
func signInAdmin(t *testing.T, a *apptest.App, address string) *http.Client {
	t.Helper()
	c := a.Client()
	u := a.SignIn(c, address)
	if err := a.Accounts.SetRole(t.Context(), u.ID, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestWebAuditLog(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	admin := signInAdmin(t, a, "admin@example.com")

	// Bob logs in through the page once with a wrong password, then right.
	bob := a.Client()
	a.SignIn(a.Client(), "bob@example.com")
	a.Post(bob, "/login", url.Values{"email": {"bob@example.com"}, "password": {"not the password"}})
	if p := a.Post(bob, "/login", url.Values{"email": {"bob@example.com"}, "password": {apptest.Password}}); p.Status != http.StatusSeeOther {
		t.Fatalf("login: %d", p.Status)
	}

	p := a.Get(admin, "/admin/audit?email=Bob@Example.com")
	if p.Status != http.StatusOK || !apptest.Contains(p.Body, "auth.login_failed", "wrong password", "127.0.0.1", "user.created") {
		t.Fatalf("Bob's events: %d\n%s", p.Status, p.Body)
	}
	if strings.Contains(p.Body, "admin@example.com") {
		t.Error("filtering by Bob's address shows the admin's events too")
	}
	if p := a.Get(admin, "/admin/audit"); !apptest.Contains(p.Body, "user.role_changed", "admin@example.com", "bob@example.com") {
		t.Errorf("all events:\n%s", p.Body)
	}
}

func TestWebAuditLogAdminsOnly(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	user := a.Client()
	a.SignIn(user, "user@example.com")
	if p := a.Get(user, "/admin/audit"); p.Status != http.StatusForbidden {
		t.Errorf("as a user: %d, want 403", p.Status)
	}
	if p := a.Get(a.Client(), "/admin/audit"); p.Status != http.StatusSeeOther {
		t.Errorf("logged out: %d, want a redirect to log in", p.Status)
	}
}

// TestWebDeletedUser checks that a deleted user's events stay, without the
// reference to them.
func TestWebDeletedUser(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	admin := signInAdmin(t, a, "admin@example.com")
	bob := a.SignIn(a.Client(), "bob@example.com")
	if err := a.Accounts.DeleteUser(t.Context(), bob.ID); err != nil {
		t.Fatal(err)
	}

	var kept, linked int
	err := a.DB.Read.QueryRowContext(t.Context(),
		"SELECT COUNT(*), COUNT(user_id) FROM audit_events WHERE event IN ('auth.login', 'user.deleted')").Scan(&kept, &linked)
	if err != nil {
		t.Fatal(err)
	}
	// Two logins (the admin's and Bob's) and Bob's deletion; only the
	// admin's login still points at a user.
	if kept != 3 || linked != 1 {
		t.Errorf("%d events kept, %d linked to a user; want 3 and 1", kept, linked)
	}
	if p := a.Get(admin, "/admin/audit"); !strings.Contains(p.Body, "user.deleted") || strings.Contains(p.Body, "bob@example.com") {
		t.Errorf("log after deleting Bob:\n%s", p.Body)
	}
}
