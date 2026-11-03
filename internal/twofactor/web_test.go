package twofactor

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
)

func newWebApp(t *testing.T) (*apptest.App, *Service) {
	t.Helper()
	a, svc := newApp(t)
	rd := a.Renderer()
	accountPages, err := auth.NewWeb(a.Accounts, rd, a.Limiter)
	if err != nil {
		t.Fatal(err)
	}
	accountPages.Routes(a.Mux)
	pages, err := NewWeb(svc, rd, a.Limiter)
	if err != nil {
		t.Fatal(err)
	}
	pages.Routes(a.Mux)
	return a, svc
}

var (
	secretField    = regexp.MustCompile(`name="secret" value="([A-Z2-7]+)"`)
	challengeField = regexp.MustCompile(`name="challenge" value="([^"]+)"`)
)

// field returns the first group pattern matches in body, failing the test if
// there is none.
func field(t *testing.T, pattern *regexp.Regexp, body string) string {
	t.Helper()
	m := pattern.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no %s in:\n%s", pattern, body)
	}
	return html.UnescapeString(m[1])
}

func TestWebTwoFactor(t *testing.T) {
	t.Parallel()
	a, _ := newWebApp(t)
	if p := a.Get(a.Client(), "/account/two-factor"); p.Status != http.StatusSeeOther {
		t.Errorf("settings page logged out: %d, want a redirect to log in", p.Status)
	}
	laptop, phone := a.Client(), a.Client()
	u := a.SignIn(laptop, "ada@example.com")
	if _, err := a.Accounts.Login(t.Context(), u.Email, apptest.Password, client, auth.BrowserSession); err != nil {
		t.Fatal(err)
	}
	a.SignIn(phone, "bob@example.com") // someone else's session survives

	p := a.Get(laptop, "/account/two-factor")
	if p.Status != http.StatusOK || !apptest.Contains(p.Body, `src="data:image/png;base64,`, "Turn on two-factor login") {
		t.Fatalf("settings page: %d\n%s", p.Status, p.Body)
	}
	secret := field(t, secretField, p.Body)

	// A wrong code keeps the same secret, so the app needn't scan again.
	p = a.Post(laptop, "/account/two-factor/enable", url.Values{"secret": {secret}, "code": {"000000"}, "password": {apptest.Password}})
	if p.Status != http.StatusUnprocessableEntity || field(t, secretField, p.Body) != secret {
		t.Fatalf("wrong code: %d\n%s", p.Status, p.Body)
	}
	p = a.Post(laptop, "/account/two-factor/enable", url.Values{"secret": {secret}, "code": {codeFor(t, secret, 0)}, "password": {apptest.Password}})
	codes := recoveryCodePattern.FindAllString(p.Body, -1)
	if p.Status != http.StatusOK || len(codes) != 10 {
		t.Fatalf("enable: %d, %d codes\n%s", p.Status, len(codes), p.Body)
	}
	if p := a.Get(laptop, "/account/two-factor"); !apptest.Contains(p.Body, "Two-factor login is on", "You have 10 left") {
		t.Errorf("settings page when on:\n%s", p.Body)
	}

	// Logging in now asks for a code after the password.
	browser := a.Client()
	p = a.Post(browser, "/login", url.Values{"email": {u.Email}, "password": {apptest.Password}, "remember": {"1"}, "next": {"/account"}})
	if p.Status != http.StatusOK || !apptest.Contains(p.Body, "Enter your code", `name="remember" value="1"`) {
		t.Fatalf("login: %d\n%s", p.Status, p.Body)
	}
	challenge := field(t, challengeField, p.Body)
	step := func(code string) apptest.Page {
		return a.Post(browser, "/login/code", url.Values{"challenge": {challenge}, "code": {code}, "remember": {"1"}, "next": {"/account"}})
	}
	if p := step("000000"); p.Status != http.StatusUnauthorized || !strings.Contains(html.UnescapeString(p.Body), "That code isn't right.") {
		t.Errorf("wrong code: %d\n%s", p.Status, p.Body)
	}
	p = step(codeFor(t, secret, 1))
	if p.Status != http.StatusSeeOther || p.Location != "/account" || !strings.Contains(p.Header.Get("Set-Cookie"), "Max-Age=") {
		t.Fatalf("right code: %d %q, Set-Cookie %q", p.Status, p.Location, p.Header.Get("Set-Cookie"))
	}
	if p := a.Get(browser, "/account"); p.Status != http.StatusOK {
		t.Errorf("account after logging in: %d", p.Status)
	}
	if p := a.Post(a.Client(), "/login/code", url.Values{"challenge": {"forged"}, "code": {codes[0]}}); p.Status != http.StatusUnauthorized ||
		!apptest.Contains(p.Body, "That took too long. Log in again.", `action="/login"`) {
		t.Errorf("forged challenge: %d\n%s", p.Status, p.Body)
	}

	// Turning it on signed out the other session.
	if p := a.Get(laptop, "/account"); p.Status != http.StatusOK {
		t.Errorf("the session that turned it on: %d", p.Status)
	}
	if p := a.Get(phone, "/account"); p.Status != http.StatusOK {
		t.Errorf("someone else's session: %d", p.Status)
	}
	if devices, _ := a.Accounts.Devices(t.Context(), u, ""); len(devices) != 2 {
		t.Errorf("ada's sessions = %d, want 2 (the laptop and the new login)", len(devices))
	}

	// New recovery codes, then turning it off, need the password.
	p = a.Post(laptop, "/account/two-factor/recovery-codes", url.Values{"password": {"not the password"}})
	if p.Status != http.StatusUnprocessableEntity {
		t.Errorf("new codes, wrong password: %d", p.Status)
	}
	if p := a.Post(laptop, "/account/two-factor/recovery-codes", url.Values{"password": {apptest.Password}}); len(recoveryCodePattern.FindAllString(p.Body, -1)) != 10 {
		t.Errorf("new codes: %d\n%s", p.Status, p.Body)
	}
	if p := a.Post(laptop, "/account/two-factor/disable", url.Values{"password": {"not the password"}}); p.Status != http.StatusUnprocessableEntity {
		t.Errorf("turn off, wrong password: %d", p.Status)
	}
	if p := a.Post(laptop, "/account/two-factor/disable", url.Values{"password": {apptest.Password}}); p.Status != http.StatusSeeOther {
		t.Errorf("turn off: %d", p.Status)
	}
	if p := a.Post(a.Client(), "/login", url.Values{"email": {u.Email}, "password": {apptest.Password}}); p.Status != http.StatusSeeOther {
		t.Errorf("login after turning off: %d, want a redirect", p.Status)
	}
}
