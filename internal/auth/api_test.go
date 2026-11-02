package auth_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
)

const apiPassword = "correct horse battery"

func newAPIApp(t *testing.T) *apptest.App {
	t.Helper()
	a := apptest.New(t)
	auth.NewAPI(a.Accounts, a.Limiter).Routes(a.Mux)
	return a
}

type me struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

func TestAPIAccountLifecycle(t *testing.T) {
	t.Parallel()
	a := newAPIApp(t)
	browser := a.Client()

	r := a.JSON(browser, "POST", "/api/auth/register", "", map[string]string{"email": "ada@example.com", "password": apiPassword})
	if r.Status != http.StatusAccepted {
		t.Fatalf("register: %d %s", r.Status, r.Body)
	}
	token := a.TokenFrom("ada@example.com", "/verify-email")
	if r := a.JSON(browser, "POST", "/api/auth/verify-email", "", map[string]string{"token": token}); r.Status != http.StatusNoContent {
		t.Fatalf("verify: %d %s", r.Status, r.Body)
	}

	// A browser frontend logs in with a cookie.
	r = a.JSON(browser, "POST", "/api/auth/login", "", map[string]any{"email": "ada@example.com", "password": apiPassword})
	if r.Status != http.StatusNoContent || !strings.Contains(r.Header.Get("Set-Cookie"), "__Host-session=") {
		t.Fatalf("login: %d, Set-Cookie %q", r.Status, r.Header.Get("Set-Cookie"))
	}
	var got me
	a.Decode(a.JSON(browser, "GET", "/api/auth/me", "", nil), &got)
	if got.Email != "ada@example.com" || !got.EmailVerified {
		t.Errorf("me (cookie) = %+v", got)
	}

	// Other clients get a bearer token.
	var issued struct {
		Token     string `json:"token"`
		TokenType string `json:"token_type"`
	}
	a.Decode(a.JSON(a.Client(), "POST", "/api/auth/token", "", map[string]string{"email": "ada@example.com", "password": apiPassword}), &issued)
	if issued.Token == "" || issued.TokenType != "Bearer" {
		t.Fatalf("token response: %+v", issued)
	}
	cli := a.Client()
	if r := a.JSON(cli, "GET", "/api/auth/me", issued.Token, nil); r.Status != http.StatusOK {
		t.Errorf("me (bearer): %d %s", r.Status, r.Body)
	}

	// Changing the password keeps this session and ends the others.
	if r := a.JSON(browser, "POST", "/api/auth/change-password", "", map[string]string{
		"current_password": apiPassword, "new_password": "a brand new password",
	}); r.Status != http.StatusNoContent {
		t.Fatalf("change password: %d %s", r.Status, r.Body)
	}
	if r := a.JSON(cli, "GET", "/api/auth/me", issued.Token, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("bearer token survived the password change: %d", r.Status)
	}

	if r := a.JSON(browser, "POST", "/api/auth/logout", "", nil); r.Status != http.StatusNoContent {
		t.Errorf("logout: %d", r.Status)
	}
	if r := a.JSON(browser, "GET", "/api/auth/me", "", nil); r.Status != http.StatusUnauthorized {
		t.Errorf("me after logout: %d", r.Status)
	}
}

func TestAPIErrors(t *testing.T) {
	t.Parallel()
	a := newAPIApp(t)
	c := a.Client()

	tests := []struct {
		name   string
		body   any
		path   string
		status int
	}{
		{"malformed JSON", "{", "/api/auth/register", http.StatusBadRequest},
		{"unknown field", map[string]string{"email": "ada@example.com", "pasword": "typo"}, "/api/auth/register", http.StatusBadRequest},
		{"invalid input", map[string]string{"email": "nope", "password": "short"}, "/api/auth/register", http.StatusUnprocessableEntity},
		{"wrong credentials", map[string]string{"email": "nobody@example.com", "password": apiPassword}, "/api/auth/login", http.StatusUnauthorized},
		{"bad link", map[string]string{"token": "nonsense"}, "/api/auth/verify-email", http.StatusBadRequest},
	}
	for _, tt := range tests {
		r := a.JSON(c, "POST", tt.path, "", tt.body)
		if r.Status != tt.status || r.Header.Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: %d %s (%s), want %d problem+json", tt.name, r.Status, r.Header.Get("Content-Type"), r.Body, tt.status)
		}
	}

	var p httpx.Problem
	a.Decode(a.JSON(c, "POST", "/api/auth/register", "", map[string]string{"email": "nope", "password": "short"}), &p)
	if p.Errors["email"] == "" || p.Errors["password"] == "" {
		t.Errorf("field errors missing: %+v", p)
	}
	if r := a.JSON(c, "GET", "/api/auth/me", "", nil); r.Status != http.StatusUnauthorized {
		t.Errorf("me without a session: %d", r.Status)
	}
}

func TestAPIResetAndDelete(t *testing.T) {
	t.Parallel()
	a := newAPIApp(t)
	c := a.Client()
	a.JSON(c, "POST", "/api/auth/register", "", map[string]string{"email": "ada@example.com", "password": apiPassword})

	if r := a.JSON(c, "POST", "/api/auth/forgot-password", "", map[string]string{"email": "ada@example.com"}); r.Status != http.StatusAccepted {
		t.Fatalf("forgot: %d", r.Status)
	}
	token := a.TokenFrom("ada@example.com", "/reset-password")
	if r := a.JSON(c, "POST", "/api/auth/reset-password", "", map[string]string{"token": token, "password": "a brand new password"}); r.Status != http.StatusNoContent {
		t.Fatalf("reset: %d %s", r.Status, r.Body)
	}
	if r := a.JSON(c, "POST", "/api/auth/login", "", map[string]string{"email": "ada@example.com", "password": "a brand new password"}); r.Status != http.StatusNoContent {
		t.Fatalf("login with the new password: %d", r.Status)
	}
	if r := a.JSON(c, "POST", "/api/auth/delete-account", "", map[string]string{"password": "a brand new password"}); r.Status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", r.Status, r.Body)
	}
	if r := a.JSON(c, "GET", "/api/auth/me", "", nil); r.Status != http.StatusUnauthorized {
		t.Errorf("me after deleting the account: %d", r.Status)
	}
}
