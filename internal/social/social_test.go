package social

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// fakeProvider stands in for Google. A test approves a consent page URL for
// an account, as someone clicking "Allow" would; the token endpoint then
// checks PKCE before handing out that account.
type fakeProvider struct {
	*httptest.Server
	mu     sync.Mutex
	grants map[string]grant // by code
}

type grant struct {
	challenge string
	acct      Account
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	f := &fakeProvider{grants: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		g, ok := f.grants[r.FormValue("code")]
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(r.FormValue("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + r.FormValue("code") + `","token_type":"Bearer"}`))
	})
	mux.HandleFunc("GET /v1/userinfo", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		g, ok := f.grants[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		f.mu.Unlock()
		if !ok {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"` + g.acct.Subject + `","email":"` + g.acct.Email + `","email_verified":` +
			map[bool]string{true: "true", false: "false"}[g.acct.EmailVerified] + `,"name":"` + g.acct.Name + `"}`))
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

// approve returns the callback URL a consent page sends acct back to.
func (f *fakeProvider) approve(t *testing.T, consentPage string, acct Account) string {
	t.Helper()
	u, err := url.Parse(consentPage)
	if err != nil || !strings.HasPrefix(consentPage, f.URL) || u.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("consent page %q", consentPage)
	}
	q := u.Query()
	code := "code-" + q.Get("state")
	f.mu.Lock()
	f.grants[code] = grant{challenge: q.Get("code_challenge"), acct: acct}
	f.mu.Unlock()
	return q.Get("redirect_uri") + "?" + url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
}

// newApp starts an apptest.App with social login on, Google played by a
// fakeProvider. options are apptest.New's.
func newApp(t *testing.T, options ...func(*auth.Options)) (*apptest.App, *Service, *fakeProvider) {
	t.Helper()
	a := apptest.New(t, options...)
	svc, err := NewService(a.DB, a.Accounts, a.Logger, Options{
		AppName: "Test", BaseURL: a.Server.URL, Google: Credentials{ClientID: "id", ClientSecret: "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := newFakeProvider(t)
	svc.providers[0].oauth.Endpoint = oauth2.Endpoint{AuthURL: f.URL + "/authorize", TokenURL: f.URL + "/token"}
	svc.providers[0].apiURL = f.URL
	return a, svc, f
}

var ada = Account{Subject: "g-ada", Email: "ada@example.com", EmailVerified: true, Name: "Ada Lovelace"}

func TestSignIn(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("new account", func(t *testing.T) {
		t.Parallel()
		a, svc, _ := newApp(t)
		id, err := svc.SignIn(ctx, "google", ada, "Europe/London")
		if err != nil {
			t.Fatal(err)
		}
		u, err := a.Accounts.UserByEmail(ctx, ada.Email)
		if err != nil || u.ID != id || u.Name != "Ada Lovelace" || u.TimeZone != "Europe/London" || !u.EmailVerified || u.HasPassword {
			t.Fatalf("user = %+v, %v", u, err)
		}
		if again, err := svc.SignIn(ctx, "google", ada, ""); err != nil || again != id {
			t.Errorf("signing in again: %v, %v; want %v", again, err, id)
		}
		// No password: logging in with one fails, and the current-password
		// check says to set one.
		if _, err := a.Accounts.Login(ctx, ada.Email, "", auth.Client{}, auth.BrowserSession); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("password login: err = %v", err)
		}
		if errs, ok := errors.AsType[validate.Errors](a.Accounts.CheckCurrentPassword(ctx, u, "password", "anything")); !ok ||
			!strings.Contains(errs["password"], "don't have a password") {
			t.Errorf("current password check: %v", errs)
		}
		if sent := a.SentTo(ada.Email); len(sent) != 0 {
			t.Errorf("signing up sent %d emails, want none", len(sent))
		}
	})

	t.Run("existing account", func(t *testing.T) {
		t.Parallel()
		a, svc, _ := newApp(t)
		u, err := a.Accounts.CreateUser(ctx, ada.Email, apptest.Password, auth.RoleUser, true)
		if err != nil {
			t.Fatal(err)
		}
		if id, err := svc.SignIn(ctx, "google", ada, ""); err != nil || id != u.ID {
			t.Fatalf("SignIn = %v, %v; want %v", id, err, u.ID)
		}
		if _, err := a.Accounts.Login(ctx, ada.Email, apptest.Password, auth.Client{}, auth.BrowserSession); err != nil {
			t.Errorf("the password stopped working: %v", err)
		}
		if sent := a.SentTo(ada.Email); len(sent) != 1 || sent[0].Subject != "Google was connected to your account" {
			t.Errorf("emails: %+v", sent)
		}
	})

	t.Run("unconfirmed account", func(t *testing.T) {
		t.Parallel()
		a, svc, _ := newApp(t)
		u, err := a.Accounts.CreateUser(ctx, ada.Email, apptest.Password, auth.RoleUser, false)
		if err != nil {
			t.Fatal(err)
		}
		token, err := a.Accounts.Login(ctx, ada.Email, apptest.Password, auth.Client{}, auth.BrowserSession)
		if err != nil {
			t.Fatal(err)
		}
		// They also asked to move the account to an address of their own.
		if err := a.Accounts.RequestEmailChange(ctx, u, apptest.Password, "mallory@example.com"); err != nil {
			t.Fatal(err)
		}
		change := a.TokenFrom("mallory@example.com", "/confirm-email")
		if id, err := svc.SignIn(ctx, "google", ada, ""); err != nil || id != u.ID {
			t.Fatalf("SignIn = %v, %v; want %v", id, err, u.ID)
		}
		if err := a.Accounts.ConfirmEmailChange(ctx, change); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("their email change still works: %v", err)
		}
		if _, err := a.Accounts.Authenticate(ctx, token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Errorf("whoever made the account is still logged in: %v", err)
		}
		if _, err := a.Accounts.Login(ctx, ada.Email, apptest.Password, auth.Client{}, auth.BrowserSession); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("their password still works: %v", err)
		}
		if u, _ := a.Accounts.UserByEmail(ctx, ada.Email); !u.EmailVerified {
			t.Error("the address isn't confirmed")
		}
	})

	t.Run("unverified address", func(t *testing.T) {
		t.Parallel()
		_, svc, _ := newApp(t)
		unverified := ada
		unverified.EmailVerified = false
		if _, err := svc.SignIn(ctx, "google", unverified, ""); !errors.Is(err, ErrUnverifiedEmail) {
			t.Errorf("err = %v, want ErrUnverifiedEmail", err)
		}
	})

	t.Run("invite only", func(t *testing.T) {
		t.Parallel()
		a, svc, _ := newApp(t, func(o *auth.Options) { o.InviteOnly = true })
		if _, err := svc.SignIn(ctx, "google", ada, ""); !errors.Is(err, auth.ErrInvitationNeeded) {
			t.Errorf("uninvited: err = %v, want ErrInvitationNeeded", err)
		}
		if err := a.Accounts.Invite(ctx, ada.Email); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SignIn(ctx, "google", ada, ""); err != nil {
			t.Errorf("invited: %v", err)
		}
	})
}

func TestConnectAndDisconnect(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	a, svc, _ := newApp(t)
	bob, err := a.Accounts.CreateUser(ctx, "bob@example.com", apptest.Password, auth.RoleUser, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Connect(ctx, bob, "google", ada); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := svc.Connect(ctx, bob, "google", ada); err != nil {
		t.Errorf("connecting the same account again: %v", err)
	}
	other := Account{Subject: "g-other", Email: "bob@gmail.com", EmailVerified: true}
	if err := svc.Connect(ctx, bob, "google", other); !errors.Is(err, ErrAlreadyConnected) {
		t.Errorf("a second Google account: err = %v, want ErrAlreadyConnected", err)
	}
	carol, err := a.Accounts.CreateUser(ctx, "carol@example.com", apptest.Password, auth.RoleUser, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Connect(ctx, carol, "google", ada); !errors.Is(err, ErrLinkedElsewhere) {
		t.Errorf("someone else's: err = %v, want ErrLinkedElsewhere", err)
	}
	if ids, err := svc.Identities(ctx, bob); err != nil || len(ids) != 1 || ids[0].Email != ada.Email || ids[0].Provider.Name != "Google" {
		t.Errorf("identities = %+v, %v", ids, err)
	}
	if err := svc.Disconnect(ctx, bob, "google"); err != nil {
		t.Errorf("disconnect: %v", err)
	}
	if ids, _ := svc.Identities(ctx, bob); len(ids) != 0 {
		t.Errorf("still connected: %+v", ids)
	}

	// Someone without a password keeps their only way in.
	id, err := svc.SignIn(ctx, "google", other, "")
	if err != nil {
		t.Fatal(err)
	}
	dave, _ := a.Accounts.UserByEmail(ctx, other.Email)
	if dave.ID != id {
		t.Fatalf("user = %+v", dave)
	}
	if err := svc.Disconnect(ctx, dave, "google"); !errors.Is(err, ErrLastWayIn) {
		t.Errorf("disconnecting the only way in: err = %v, want ErrLastWayIn", err)
	}
}
