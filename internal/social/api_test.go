package social

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/auth"
)

func TestAPI(t *testing.T) {
	t.Parallel()
	a, svc, f := newApp(t)
	auth.NewAPI(a.Accounts).Routes(a.Mux)
	NewAPI(svc).Routes(a.Mux)

	var list struct{ Providers []providerJSON }
	a.Decode(a.JSON(a.Client(), "GET", "/api/auth/social", "", nil), &list)
	if len(list.Providers) != 1 || list.Providers[0] != (providerJSON{ID: "google", Name: "Google"}) {
		t.Errorf("providers = %+v", list.Providers)
	}

	// The frontend sends the browser to the provider and gets it back at
	// next, logged in.
	browser := a.Client()
	start := a.JSON(browser, "GET", "/api/auth/social/google?next=/app&time_zone=Africa/Johannesburg", "", nil)
	if start.Status != http.StatusSeeOther {
		t.Fatalf("start: %d %s", start.Status, start.Body)
	}
	callback := strings.TrimPrefix(f.approve(t, start.Header.Get("Location"), ada), a.Server.URL)
	if !strings.HasPrefix(callback, "/api/auth/social/google/callback?") {
		t.Fatalf("callback = %s", callback)
	}
	r := a.JSON(browser, "GET", callback, "", nil)
	if r.Status != http.StatusSeeOther || r.Header.Get("Location") != "/app" {
		t.Fatalf("callback: %d %q", r.Status, r.Header.Get("Location"))
	}
	var me struct {
		Email       string `json:"email"`
		TimeZone    string `json:"time_zone"`
		HasPassword bool   `json:"has_password"`
	}
	a.Decode(a.JSON(browser, "GET", "/api/auth/me", "", nil), &me)
	if me.Email != ada.Email || me.TimeZone != "Africa/Johannesburg" || me.HasPassword {
		t.Errorf("me = %+v", me)
	}

	// Failures come back to next with an error.
	if r := a.JSON(a.Client(), "GET", callback, "", nil); r.Header.Get("Location") != "/?error=expired" {
		t.Errorf("callback without the cookie: %q", r.Header.Get("Location"))
	}

	var ids struct {
		Identities []struct{ Provider, Email string }
	}
	a.Decode(a.JSON(browser, "GET", "/api/auth/social/identities", "", nil), &ids)
	if len(ids.Identities) != 1 || ids.Identities[0].Provider != "google" || ids.Identities[0].Email != ada.Email {
		t.Errorf("identities = %+v", ids)
	}
	if r := a.JSON(browser, "DELETE", "/api/auth/social/identities/google", "", nil); r.Status != http.StatusConflict {
		t.Errorf("disconnecting the only way in: %d", r.Status)
	}
}
