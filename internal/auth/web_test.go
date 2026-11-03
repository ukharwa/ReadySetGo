package auth_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
)

const password = "correct horse battery"

func newWebApp(t *testing.T) *apptest.App {
	t.Helper()
	a := apptest.New(t)
	pages, err := auth.NewWeb(a.Accounts, a.Renderer(), a.Limiter)
	if err != nil {
		t.Fatal(err)
	}
	pages.Routes(a.Mux)
	return a
}

// signUp registers address and logs c in.
func signUp(t *testing.T, a *apptest.App, c *http.Client, address string) {
	t.Helper()
	if p := a.Post(c, "/register", url.Values{"email": {address}, "password": {password}}); p.Status != http.StatusSeeOther {
		t.Fatalf("register: status %d\n%s", p.Status, p.Body)
	}
	logIn(t, a, c, address, password)
}

func logIn(t *testing.T, a *apptest.App, c *http.Client, address, pw string) {
	t.Helper()
	if p := a.Post(c, "/login", url.Values{"email": {address}, "password": {pw}}); p.Status != http.StatusSeeOther {
		t.Fatalf("login as %s: status %d\n%s", address, p.Status, p.Body)
	}
}

func loggedIn(t *testing.T, a *apptest.App, c *http.Client) bool {
	t.Helper()
	return a.Get(c, "/account").Status == http.StatusOK
}

func TestWebSignUpVerifyLogOut(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	c := a.Client()

	p := a.Post(c, "/register", url.Values{"email": {" Ada@Example.com "}, "password": {password}})
	if p.Status != http.StatusSeeOther || p.Location != "/login" {
		t.Fatalf("register: %d → %q", p.Status, p.Location)
	}
	p = a.Post(c, "/login", url.Values{"email": {"ada@example.com"}, "password": {password}, "next": {"/account"}})
	if p.Status != http.StatusSeeOther || p.Location != "/account" {
		t.Fatalf("login: %d → %q", p.Status, p.Location)
	}
	if p := a.Get(c, "/account"); !apptest.Contains(p.Body, "ada@example.com", "Send a new link") {
		t.Errorf("account page doesn't show an unverified ada@example.com:\n%s", p.Body)
	}

	// The link opens a confirmation page; only submitting it verifies.
	token := a.TokenFrom("ada@example.com", "/verify-email")
	if p := a.Get(c, "/verify-email?token="+token); !strings.Contains(p.Body, `value="`+token+`"`) {
		t.Fatalf("verify page doesn't carry the token:\n%s", p.Body)
	}
	if p := a.Post(c, "/verify-email", url.Values{"token": {token}}); p.Status != http.StatusSeeOther {
		t.Fatalf("verify: status %d\n%s", p.Status, p.Body)
	}
	if p := a.Get(c, "/account"); strings.Contains(p.Body, "Send a new link") {
		t.Error("account still unverified after confirming")
	}
	if p := a.Post(c, "/verify-email", url.Values{"token": {token}}); p.Status != http.StatusBadRequest {
		t.Errorf("reusing the link: status %d, want 400", p.Status)
	}

	a.Post(c, "/logout", nil)
	if p := a.Get(c, "/account"); p.Status != http.StatusSeeOther || p.Location != "/login?next=%2Faccount" {
		t.Errorf("after logout, /account: %d → %q; want a redirect to log in", p.Status, p.Location)
	}
}

// TestWebRegisterRevealsNothing checks that signing up with a taken address
// looks the same as signing up with a new one, and tells the owner instead.
func TestWebRegisterRevealsNothing(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	first := a.Post(a.Client(), "/register", url.Values{"email": {"ada@example.com"}, "password": {password}})
	again := a.Post(a.Client(), "/register", url.Values{"email": {"ada@example.com"}, "password": {"something else entirely"}})
	if first.Status != again.Status || first.Location != again.Location {
		t.Errorf("new address: %d → %q; taken address: %d → %q", first.Status, first.Location, again.Status, again.Location)
	}
	sent := a.SentTo("ada@example.com")
	if len(sent) != 2 || !strings.Contains(sent[1].Subject, "tried to sign up") {
		t.Errorf("emails to the owner: %+v", sent)
	}
}

func TestWebRegisterValidation(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	p := a.Post(a.Client(), "/register", url.Values{"email": {"not-an-address"}, "password": {"short"}})
	if p.Status != http.StatusUnprocessableEntity || !apptest.Contains(p.Body, "valid email address", "between 12 and 256") {
		t.Errorf("status %d, body:\n%s", p.Status, p.Body)
	}
}

func TestWebLoginFailures(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	signUp(t, a, a.Client(), "ada@example.com")
	c := a.Client()

	wrong := a.Post(c, "/login", url.Values{"email": {"ada@example.com"}, "password": {"wrong password!"}})
	unknown := a.Post(c, "/login", url.Values{"email": {"nobody@example.com"}, "password": {"wrong password!"}})
	if wrong.Status != http.StatusUnauthorized || unknown.Status != http.StatusUnauthorized ||
		!strings.Contains(wrong.Body, "email or password is incorrect") || !strings.Contains(unknown.Body, "email or password is incorrect") {
		t.Errorf("wrong password: %d; unknown email: %d; want the same 401 message", wrong.Status, unknown.Status)
	}

	for range 4 { // 5 failures in all for this account from this address
		a.Post(c, "/login", url.Values{"email": {"ada@example.com"}, "password": {"wrong password!"}})
	}
	p := a.Post(c, "/login", url.Values{"email": {"ada@example.com"}, "password": {password}})
	if p.Status != http.StatusTooManyRequests || p.Header.Get("Retry-After") == "" {
		t.Errorf("6th attempt: status %d, Retry-After %q; want 429", p.Status, p.Header.Get("Retry-After"))
	}
}

func TestWebRememberMe(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	signUp(t, a, a.Client(), "ada@example.com")

	plain := a.Post(a.Client(), "/login", url.Values{"email": {"ada@example.com"}, "password": {password}})
	remembered := a.Post(a.Client(), "/login", url.Values{"email": {"ada@example.com"}, "password": {password}, "remember": {"1"}})
	if c := plain.Header.Get("Set-Cookie"); strings.Contains(c, "Max-Age") || !apptest.Contains(c, "__Host-session=", "HttpOnly", "Secure", "SameSite=Lax") {
		t.Errorf("session cookie: %s", c)
	}
	if c := remembered.Header.Get("Set-Cookie"); !strings.Contains(c, "Max-Age=2592000") {
		t.Errorf("remember-me cookie should last 30 days: %s", c)
	}
}

func TestWebLoginRedirectStaysOnSite(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	signUp(t, a, a.Client(), "ada@example.com")
	for _, next := range []string{"//evil.example", "https://evil.example", `/\evil.example`} {
		p := a.Post(a.Client(), "/login", url.Values{"email": {"ada@example.com"}, "password": {password}, "next": {next}})
		if p.Location != "/" {
			t.Errorf("next=%q redirected to %q, want /", next, p.Location)
		}
	}
}

func TestWebPasswordReset(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	elsewhere := a.Client()
	signUp(t, a, elsewhere, "ada@example.com")

	unknown := a.Post(a.Client(), "/forgot-password", url.Values{"email": {"nobody@example.com"}})
	known := a.Post(a.Client(), "/forgot-password", url.Values{"email": {"ada@example.com"}})
	if unknown.Status != known.Status || unknown.Location != known.Location {
		t.Errorf("forgot password reveals accounts: %d → %q vs %d → %q", unknown.Status, unknown.Location, known.Status, known.Location)
	}

	token := a.TokenFrom("ada@example.com", "/reset-password")
	c := a.Client()
	if p := a.Post(c, "/reset-password", url.Values{"token": {token}, "password": {"short"}}); p.Status != http.StatusUnprocessableEntity {
		t.Errorf("weak new password: status %d, want 422", p.Status)
	}
	if p := a.Post(c, "/reset-password", url.Values{"token": {token}, "password": {"a brand new password"}}); p.Status != http.StatusSeeOther {
		t.Fatalf("reset: status %d\n%s", p.Status, p.Body)
	}
	if loggedIn(t, a, elsewhere) {
		t.Error("an existing session survived the password reset")
	}
	if p := a.Post(a.Client(), "/login", url.Values{"email": {"ada@example.com"}, "password": {password}}); p.Status != http.StatusUnauthorized {
		t.Errorf("old password still works: status %d", p.Status)
	}
	logIn(t, a, c, "ada@example.com", "a brand new password")
	if p := a.Post(c, "/reset-password", url.Values{"token": {token}, "password": {"yet another password"}}); p.Status != http.StatusBadRequest {
		t.Errorf("reusing the reset link: status %d, want 400", p.Status)
	}
}

func TestWebChangePassword(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	here, elsewhere := a.Client(), a.Client()
	signUp(t, a, here, "ada@example.com")
	logIn(t, a, elsewhere, "ada@example.com", password)

	if p := a.Post(here, "/account/password", url.Values{"current_password": {"wrong one"}, "new_password": {"a brand new password"}}); p.Status != http.StatusUnprocessableEntity || !strings.Contains(p.Body, "current password") {
		t.Errorf("wrong current password: status %d", p.Status)
	}
	if p := a.Post(here, "/account/password", url.Values{"current_password": {password}, "new_password": {"a brand new password"}}); p.Status != http.StatusSeeOther {
		t.Fatalf("change password: status %d\n%s", p.Status, p.Body)
	}
	if !loggedIn(t, a, here) || loggedIn(t, a, elsewhere) {
		t.Error("want this session kept and the other one logged out")
	}
}

func TestWebChangeEmail(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	c := a.Client()
	signUp(t, a, c, "ada@example.com")

	if p := a.Post(c, "/account/email", url.Values{"email": {"ada@new.example"}, "password": {password}}); p.Status != http.StatusSeeOther {
		t.Fatalf("change email: status %d\n%s", p.Status, p.Body)
	}
	token := a.TokenFrom("ada@new.example", "/confirm-email")
	if p := a.Post(c, "/confirm-email", url.Values{"token": {token}}); p.Status != http.StatusSeeOther {
		t.Fatalf("confirm: status %d\n%s", p.Status, p.Body)
	}
	if p := a.Get(c, "/account"); !strings.Contains(p.Body, "ada@new.example") {
		t.Error("account page doesn't show the new address")
	}
	old := a.SentTo("ada@example.com")
	if len(old) == 0 || !strings.Contains(old[len(old)-1].Subject, "was changed") {
		t.Errorf("old address wasn't told: %+v", old)
	}
}

func TestWebDeleteAccount(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	c := a.Client()
	signUp(t, a, c, "ada@example.com")

	if p := a.Post(c, "/account/delete", url.Values{"password": {"wrong one"}}); p.Status != http.StatusUnprocessableEntity {
		t.Errorf("wrong password: status %d, want 422", p.Status)
	}
	if p := a.Post(c, "/account/delete", url.Values{"password": {password}}); p.Status != http.StatusSeeOther {
		t.Fatalf("delete: status %d\n%s", p.Status, p.Body)
	}
	if loggedIn(t, a, c) {
		t.Error("still logged in after deleting the account")
	}
	if p := a.Post(a.Client(), "/login", url.Values{"email": {"ada@example.com"}, "password": {password}}); p.Status != http.StatusUnauthorized {
		t.Errorf("deleted account can log in: status %d", p.Status)
	}
}

// TestWebPagesRender loads every page, logged out and in, so a template that
// fails at render time fails here rather than for a user.
func TestWebPagesRender(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	c := a.Client()
	for _, path := range []string{
		"/register", "/login", "/forgot-password",
		"/reset-password?token=x", "/verify-email?token=x", "/confirm-email?token=x",
	} {
		if p := a.Get(c, path); p.Status != http.StatusOK || !strings.Contains(p.Body, "</html>") {
			t.Errorf("%s: status %d", path, p.Status)
		}
	}
	signUp(t, a, c, "ada@example.com")
	if p := a.Get(c, "/account"); p.Status != http.StatusOK || !strings.Contains(p.Body, "</html>") {
		t.Errorf("/account: status %d", p.Status)
	}
}

var deviceLink = regexp.MustCompile(`/account/devices/([0-9a-f-]{36})/sign-out`)

// otherDevices returns the IDs of the devices c's account page offers to
// sign out: every one but c's own.
func otherDevices(t *testing.T, a *apptest.App, c *http.Client) []string {
	t.Helper()
	p := a.Get(c, "/account")
	if strings.Count(p.Body, "This device") != 1 {
		t.Fatalf("account page doesn't mark this device once:\n%s", p.Body)
	}
	var ids []string
	for _, m := range deviceLink.FindAllStringSubmatch(p.Body, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

func TestWebDevices(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	here, phone, laptop := a.Client(), a.Client(), a.Client()
	signUp(t, a, here, "ada@example.com")
	logIn(t, a, phone, "ada@example.com", password)

	others := otherDevices(t, a, here)
	if len(others) != 1 {
		t.Fatalf("other devices: %v, want the phone's", others)
	}
	phoneID := others[0]

	// Someone else can't sign out Ada's phone.
	bob := a.Client()
	signUp(t, a, bob, "bob@example.com")
	a.Post(bob, "/account/devices/"+phoneID+"/sign-out", nil)
	if !loggedIn(t, a, phone) {
		t.Fatal("another user signed out Ada's phone")
	}

	if p := a.Post(here, "/account/devices/"+phoneID+"/sign-out", nil); p.Status != http.StatusSeeOther {
		t.Fatalf("sign out the phone: status %d\n%s", p.Status, p.Body)
	}
	if loggedIn(t, a, phone) || !loggedIn(t, a, here) {
		t.Fatal("want the phone signed out and this device kept")
	}
	if p := a.Post(here, "/account/devices/"+phoneID+"/sign-out", nil); p.Status != http.StatusSeeOther {
		t.Errorf("signing out a device twice: status %d", p.Status)
	}
	if p := a.Post(here, "/account/devices/not-an-id/sign-out", nil); p.Status != http.StatusNotFound {
		t.Errorf("malformed device ID: status %d, want 404", p.Status)
	}

	logIn(t, a, phone, "ada@example.com", password)
	logIn(t, a, laptop, "ada@example.com", password)
	if p := a.Post(here, "/account/devices/sign-out-others", nil); p.Status != http.StatusSeeOther {
		t.Fatalf("sign out the others: status %d\n%s", p.Status, p.Body)
	}
	if loggedIn(t, a, phone) || loggedIn(t, a, laptop) || !loggedIn(t, a, here) {
		t.Error("want every other device signed out and this one kept")
	}
	if others := otherDevices(t, a, here); len(others) != 0 {
		t.Errorf("devices left besides this one: %v", others)
	}
}

func TestWebProfile(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	c := a.Client()
	p := a.Post(c, "/register", url.Values{
		"name": {"  Ada   Lovelace "}, "email": {"ada@example.com"}, "password": {password}, "time_zone": {"Not/AZone"},
	})
	if p.Status != http.StatusSeeOther {
		t.Fatalf("register: status %d\n%s", p.Status, p.Body)
	}
	if sent := a.SentTo("ada@example.com"); len(sent) != 1 || !strings.Contains(sent[0].Body, "Hello Ada Lovelace,") {
		t.Errorf("verification email doesn't greet Ada by name: %+v", sent)
	}
	logIn(t, a, c, "ada@example.com", password)
	page := a.Get(c, "/account")
	if !apptest.Contains(page.Body, `>Ada Lovelace</a>`, `value="Ada Lovelace"`, `value="UTC"`) {
		t.Errorf("want the name in the nav and form, and an unknown zone replaced by UTC:\n%s", page.Body)
	}

	p = a.Post(c, "/account/profile", url.Values{"name": {"Ada"}, "time_zone": {"Atlantis/Capital"}})
	if p.Status != http.StatusUnprocessableEntity || !strings.Contains(p.Body, "Choose a time zone") {
		t.Errorf("unknown time zone: status %d\n%s", p.Status, p.Body)
	}
	if p := a.Post(c, "/account/profile", url.Values{"name": {"Ada"}, "time_zone": {"Asia/Kolkata"}}); p.Status != http.StatusSeeOther {
		t.Fatalf("save profile: status %d\n%s", p.Status, p.Body)
	}

	// Times show in the chosen zone: the device list's "last active" is
	// this login, a moment ago, in India's UTC+5:30.
	kolkata, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Now().In(kolkata)
	page = a.Get(c, "/account")
	if !strings.Contains(page.Body, now.Format("2 Jan 2006 15:04")) && !strings.Contains(page.Body, now.Add(-time.Minute).Format("2 Jan 2006 15:04")) {
		t.Errorf("device list doesn't show %s Kolkata time:\n%s", now.Format("15:04"), page.Body)
	}
}
