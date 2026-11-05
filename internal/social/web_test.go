package social

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/web"

	// readysetgo:begin module=twofactor
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"regexp"
	"time"

	"github.com/ukharwa/readysetgo/internal/twofactor"
	// readysetgo:end
)

func newWebApp(t *testing.T) (*apptest.App, *Service, *fakeProvider) {
	t.Helper()
	a, svc, f := newApp(t)
	rd := a.Renderer(func(o *web.Options) { o.Providers = []web.Provider{{ID: "google", Name: "Google"}} })
	accountPages, err := auth.NewWeb(a.Accounts, rd)
	if err != nil {
		t.Fatal(err)
	}
	accountPages.Routes(a.Mux)
	pages, err := NewWeb(svc, rd)
	if err != nil {
		t.Fatal(err)
	}
	pages.Routes(a.Mux)
	return a, svc, f
}

// logIn goes to Google from path with c, comes back as acct, and returns
// the callback's response.
func logIn(t *testing.T, a *apptest.App, f *fakeProvider, c *http.Client, path string, acct Account) apptest.Page {
	t.Helper()
	p := a.Get(c, path)
	if p.Status != http.StatusSeeOther {
		t.Fatalf("GET %s: %d", path, p.Status)
	}
	return a.Get(c, strings.TrimPrefix(f.approve(t, p.Location, acct), a.Server.URL))
}

// flash follows a redirect and returns the page's text.
func flash(a *apptest.App, c *http.Client, p apptest.Page) string {
	return html.UnescapeString(a.Get(c, p.Location).Body)
}

func TestWebLogIn(t *testing.T) {
	t.Parallel()
	a, _, f := newWebApp(t)
	if p := a.Get(a.Client(), "/login?next=/account"); !apptest.Contains(p.Body, `href="/auth/google?next=%2faccount"`, "Continue with Google") {
		t.Errorf("login page:\n%s", p.Body)
	}

	c := a.Client()
	p := logIn(t, a, f, c, "/auth/google?next=/account&time_zone=Europe/London", ada)
	if p.Status != http.StatusSeeOther || p.Location != "/account" || !strings.Contains(strings.Join(p.Header.Values("Set-Cookie"), "\n"), auth.SessionCookie) {
		t.Fatalf("callback: %d %q, Set-Cookie %q", p.Status, p.Location, p.Header.Values("Set-Cookie"))
	}
	if p := a.Get(c, "/account"); p.Status != http.StatusOK || !strings.Contains(p.Body, ada.Email) {
		t.Errorf("account page: %d", p.Status)
	}
	if u, _ := a.Accounts.UserByEmail(t.Context(), ada.Email); u.TimeZone != "Europe/London" {
		t.Errorf("time zone = %q", u.TimeZone)
	}

	// A callback needs the cookie from its own start, in the same browser.
	other := a.Client()
	start := a.Get(other, "/auth/google")
	callback := strings.TrimPrefix(f.approve(t, start.Location, ada), a.Server.URL)
	stranger := a.Client()
	if p := a.Get(stranger, callback); p.Location != "/login" || !strings.Contains(flash(a, stranger, p), "That took too long") {
		t.Errorf("callback in another browser: %d %q", p.Status, p.Location)
	}
	q, _ := url.Parse(start.Location)
	cancelled := "/auth/google/callback?" + url.Values{"error": {"access_denied"}, "state": {q.Query().Get("state")}}.Encode()
	if p := a.Get(other, cancelled); p.Location != "/login" || !strings.Contains(flash(a, other, p), "Logging in was cancelled.") {
		t.Errorf("cancelled: %d %q", p.Status, p.Location)
	}
	if p := a.Get(a.Client(), "/auth/nope"); p.Status != http.StatusNotFound {
		t.Errorf("unknown provider: %d", p.Status)
	}
}

func TestWebConnect(t *testing.T) {
	t.Parallel()
	a, _, f := newWebApp(t)
	c := a.Client()
	a.SignIn(c, "bob@example.com")
	if p := a.Get(c, "/account/sign-in"); !apptest.Contains(p.Body, "None connected yet.", `href="/auth/google"`) {
		t.Errorf("before connecting:\n%s", p.Body)
	}
	p := logIn(t, a, f, c, "/auth/google", ada)
	if p.Location != "/account/sign-in" || !strings.Contains(flash(a, c, p), "Connected.") {
		t.Fatalf("connect: %d %q", p.Status, p.Location)
	}
	if p := a.Get(c, "/account/sign-in"); !apptest.Contains(p.Body, "Google (ada@example.com)", "Disconnect Google") {
		t.Errorf("after connecting:\n%s", p.Body)
	}
	if p := a.Post(c, "/account/sign-in/google/disconnect", nil); p.Status != http.StatusSeeOther {
		t.Errorf("disconnect: %d", p.Status)
	}
	if p := a.Get(c, "/account/sign-in"); !strings.Contains(p.Body, "None connected yet.") {
		t.Errorf("after disconnecting:\n%s", p.Body)
	}

	// Someone who signed up with Google can't disconnect their only way in.
	dave := a.Client()
	if p := logIn(t, a, f, dave, "/auth/google", Account{Subject: "g-dave", Email: "dave@example.com", EmailVerified: true}); p.Location != "/" {
		t.Fatalf("dave's login: %d %q %s", p.Status, p.Location, flash(a, dave, p))
	}
	if p := a.Get(dave, "/account/sign-in"); !strings.Contains(p.Body, "You haven't set a password") {
		t.Errorf("no password: %d\n%s", p.Status, p.Body)
	}
	p = a.Post(dave, "/account/sign-in/google/disconnect", nil)
	if !strings.Contains(flash(a, dave, p), "That's your only way to log in") {
		t.Errorf("disconnecting the only way in: %d %q", p.Status, p.Location)
	}
}

// readysetgo:begin module=twofactor
// TestWebTwoFactor checks that logging in with Google still asks for a
// two-factor code when it's on.
func TestWebTwoFactor(t *testing.T) {
	t.Parallel()
	a, _, f := newWebApp(t)
	twoFactor, err := twofactor.NewService(a.DB, a.Accounts, a.Logger, twofactor.Options{AppName: "Test", Key: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := a.Accounts.CreateUser(t.Context(), ada.Email, apptest.Password, auth.RoleUser, true)
	if err != nil {
		t.Fatal(err)
	}
	// Turn it on with a code worked out as an authenticator app would.
	s := twoFactor.NewSetup(u)
	codes, err := twoFactor.Enable(t.Context(), u, s.Secret, totpNow(t, s.Secret), apptest.Password)
	if err != nil {
		t.Fatal(err)
	}

	c := a.Client()
	p := logIn(t, a, f, c, "/auth/google?next=/account", ada)
	m := regexp.MustCompile(`name="challenge" value="([^"]+)"`).FindStringSubmatch(p.Body)
	if p.Status != http.StatusOK || m == nil || !strings.Contains(p.Body, "Enter your code") {
		t.Fatalf("callback with two-factor on: %d\n%s", p.Status, p.Body)
	}
	p = a.Post(c, "/login/code", url.Values{"challenge": {html.UnescapeString(m[1])}, "code": {codes[0]}, "next": {"/account"}})
	if p.Status != http.StatusSeeOther || p.Location != "/account" {
		t.Errorf("code step: %d %q", p.Status, p.Location)
	}
}

// totpNow returns secret's current code (RFC 6238).
func totpNow(t *testing.T, secret string) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha1.New, key)
	mac.Write(binary.BigEndian.AppendUint64(nil, uint64(time.Now().Unix()/30)))
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:])&0x7fffffff)%1_000_000)
}

// readysetgo:end
