package audit_test

import (
	"net/http"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/audit"
	"github.com/ukharwa/readysetgo/internal/auth"
)

func TestAPIAuditLog(t *testing.T) {
	t.Parallel()
	a := apptest.New(t)
	audit.NewAPI(audit.NewService(a.DB, a.Accounts, a.Logger)).Routes(a.Mux)
	admin := a.Client()
	u := a.SignIn(admin, "admin@example.com")
	if err := a.Accounts.SetRole(t.Context(), u.ID, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	user := a.Client()
	a.SignIn(user, "user@example.com")

	var events []struct {
		Event     string  `json:"event"`
		UserID    *string `json:"user_id"`
		UserEmail string  `json:"user_email"`
		Detail    string  `json:"detail"`
	}
	r := a.JSON(admin, "GET", "/api/admin/audit-events?email=admin@example.com", "", nil)
	a.Decode(r, &events)
	// Newest first: the role change, the login, the account's creation.
	if r.Status != http.StatusOK || len(events) != 3 || events[0].Event != auth.EventRoleChanged ||
		events[0].Detail != auth.RoleAdmin || events[0].UserEmail != "admin@example.com" ||
		events[0].UserID == nil || *events[0].UserID != u.ID.String() {
		t.Errorf("admin's events: %d %+v", r.Status, events)
	}
	if r := a.JSON(user, "GET", "/api/admin/audit-events", "", nil); r.Status != http.StatusForbidden {
		t.Errorf("as a user: %d, want 403", r.Status)
	}
}
