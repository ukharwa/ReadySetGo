package admin_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ukharwa/kickstart/internal/admin"
	"github.com/ukharwa/kickstart/internal/apptest"
	"github.com/ukharwa/kickstart/internal/auth"
	"github.com/ukharwa/kickstart/internal/jobs"
)

func newWebApp(t *testing.T) *apptest.App {
	t.Helper()
	a := apptest.New(t)
	pages, err := admin.NewWeb(admin.NewService(a.DB, a.Accounts, a.Logger), a.Renderer())
	if err != nil {
		t.Fatal(err)
	}
	pages.Routes(a.Mux)
	return a
}

func TestWebUsers(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	admin, _ := signInAdmin(t, a)
	a.SignIn(a.Client(), "bob@example.com")
	a.SignIn(a.Client(), "carol_x@example.com")

	if p := a.Get(admin, "/admin/users"); p.Status != http.StatusOK || !apptest.Contains(p.Body, "admin@example.com", "bob@example.com", "carol_x@example.com") {
		t.Errorf("all users: %d\n%s", p.Status, p.Body)
	}
	if p := a.Get(admin, "/admin/users?q=BOB"); !strings.Contains(p.Body, "bob@example.com") || strings.Contains(p.Body, "carol") {
		t.Errorf("search for bob:\n%s", p.Body)
	}
	// LIKE's wildcards are matched literally.
	if p := a.Get(admin, "/admin/users?q=l_x"); !strings.Contains(p.Body, "carol_x@example.com") || strings.Contains(p.Body, "bob@") {
		t.Errorf("search for l_x:\n%s", p.Body)
	}
	if p := a.Get(admin, "/admin/users?q=%25"); !strings.Contains(p.Body, "No users") {
		t.Errorf("search for %%:\n%s", p.Body)
	}
}

func TestWebManageUser(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	admin, _ := signInAdmin(t, a)
	bobClient := a.Client()
	bob := a.SignIn(bobClient, "bob@example.com")
	page := "/admin/users/" + bob.ID.String()

	if p := a.Post(admin, page+"/role", url.Values{"role": {"admin"}}); p.Status != http.StatusSeeOther {
		t.Fatalf("promote: %d\n%s", p.Status, p.Body)
	}
	if p := a.Get(admin, page); !apptest.Contains(p.Body, "The role is changed.", `<option value="admin" selected>`) {
		t.Errorf("after promoting:\n%s", p.Body)
	}
	if p := a.Post(admin, page+"/role", url.Values{"role": {"owner"}}); p.Status != http.StatusUnprocessableEntity || !strings.Contains(p.Body, "Choose user or admin.") {
		t.Errorf("unknown role: %d", p.Status)
	}

	// Disabling logs Bob out; enabling lets him log in again.
	a.Post(admin, page+"/disable", nil)
	if p := a.Get(bobClient, "/admin/users"); p.Status != http.StatusSeeOther {
		t.Errorf("disabled Bob's session still works: %d", p.Status)
	}
	a.Post(admin, page+"/enable", nil)
	if _, err := a.Accounts.Login(t.Context(), "bob@example.com", apptest.Password, "192.0.2.1", auth.BrowserSession); err != nil {
		t.Errorf("log in after enabling: %v", err)
	}

	if p := a.Post(admin, page+"/delete", nil); p.Status != http.StatusSeeOther || p.Location != "/admin/users" {
		t.Fatalf("delete: %d → %q", p.Status, p.Location)
	}
	if p := a.Get(admin, page); p.Status != http.StatusNotFound {
		t.Errorf("deleted user's page: %d, want 404", p.Status)
	}
}

func TestWebResendVerification(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	admin, _ := signInAdmin(t, a)
	dora, err := a.Accounts.CreateUser(t.Context(), "dora@example.com", apptest.Password, auth.RoleUser, false)
	if err != nil {
		t.Fatal(err)
	}
	if p := a.Post(admin, "/admin/users/"+dora.ID.String()+"/verification", nil); p.Status != http.StatusSeeOther {
		t.Fatalf("resend: %d\n%s", p.Status, p.Body)
	}
	a.TokenFrom("dora@example.com", "/verify-email")
}

// TestWebOwnAccount checks that admins can't lock themselves out.
func TestWebOwnAccount(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	admin, me := signInAdmin(t, a)
	page := "/admin/users/" + me.ID.String()
	if p := a.Get(admin, page); !strings.Contains(p.Body, "This is your own account.") || strings.Contains(p.Body, "/disable") {
		t.Errorf("own page offers changes:\n%s", p.Body)
	}
	for _, action := range []string{"/role", "/disable", "/delete"} {
		if p := a.Post(admin, page+action, url.Values{"role": {"user"}}); p.Status != http.StatusForbidden || !strings.Contains(p.Body, "another admin can") {
			t.Errorf("POST %s on own account: %d, want 403", action, p.Status)
		}
	}
}

func TestWebAdminsOnly(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	_, me := signInAdmin(t, a)
	user := a.Client()
	a.SignIn(user, "user@example.com")
	for _, path := range []string{"/admin/users", "/admin/jobs", "/admin/users/" + me.ID.String()} {
		if p := a.Get(user, path); p.Status != http.StatusForbidden {
			t.Errorf("GET %s as a user: %d, want 403", path, p.Status)
		}
	}
	if p := a.Post(user, "/admin/users/"+me.ID.String()+"/role", url.Values{"role": {"user"}}); p.Status != http.StatusForbidden {
		t.Errorf("demoting as a user: %d, want 403", p.Status)
	}
}

func TestWebFailedJobs(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	admin, _ := signInAdmin(t, a)
	ctx := t.Context()
	if err := jobs.Enqueue(ctx, a.DB.Write, "test.broken", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Write.ExecContext(ctx, "UPDATE jobs SET failed_at = created_at, last_error = 'boom' WHERE kind = 'test.broken'"); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := a.DB.Read.QueryRowContext(ctx, "SELECT id FROM jobs WHERE kind = 'test.broken'").Scan(&id); err != nil {
		t.Fatal(err)
	}

	if p := a.Get(admin, "/admin/jobs"); !apptest.Contains(p.Body, "test.broken", "boom") {
		t.Fatalf("failed jobs:\n%s", p.Body)
	}
	if p := a.Post(admin, "/admin/jobs/"+id+"/retry", nil); p.Status != http.StatusSeeOther {
		t.Fatalf("retry: %d", p.Status)
	}
	if p := a.Get(admin, "/admin/jobs"); !strings.Contains(p.Body, "No failed jobs.") {
		t.Errorf("after retrying:\n%s", p.Body)
	}
	if p := a.Post(admin, "/admin/jobs/"+id+"/retry", nil); p.Status != http.StatusNotFound {
		t.Errorf("retrying a job that isn't failed: %d, want 404", p.Status)
	}
}
