package twofactor

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/twofactor/store"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// newApp starts an apptest.App with two-factor login hooked into its
// accounts, as the app does.
func newApp(t *testing.T) (*apptest.App, *Service) {
	t.Helper()
	a := apptest.New(t)
	svc, err := NewService(a.DB, a.Accounts, a.Logger, Options{AppName: "Test", Key: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	return a, svc
}

// codeFor returns secret's code for the time step steps after now. Turning
// two-factor login on uses up the current step, so the next login needs
// steps of 1.
func codeFor(t *testing.T, secret string, steps int64) string {
	t.Helper()
	raw, err := b32.DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	return code(raw, step(time.Now())+steps)
}

var recoveryCodePattern = regexp.MustCompile(`\b[A-Z2-7]{4}-[A-Z2-7]{4}-[A-Z2-7]{4}\b`)

// enable turns two-factor login on for u and returns their secret and
// recovery codes.
func enable(t *testing.T, svc *Service, u auth.User) (string, []string) {
	t.Helper()
	s := svc.NewSetup(u)
	codes, err := svc.Enable(t.Context(), u, s.Secret, codeFor(t, s.Secret, 0), apptest.Password)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	return s.Secret, codes
}

var client = auth.Client{IP: "192.0.2.1"}

// challenge logs u in with their password and returns the challenge for
// the code.
func challenge(t *testing.T, a *apptest.App, u auth.User) string {
	t.Helper()
	_, err := a.Accounts.Login(t.Context(), u.Email, apptest.Password, client, auth.BrowserSession)
	step, ok := errors.AsType[*auth.CodeRequiredError](err)
	if !ok {
		t.Fatalf("login with two-factor on: err = %v, want *CodeRequiredError", err)
	}
	return step.Challenge
}

func TestEnable(t *testing.T) {
	t.Parallel()
	a, svc := newApp(t)
	u, err := a.Accounts.CreateUser(t.Context(), "ada@example.com", apptest.Password, auth.RoleUser, true)
	if err != nil {
		t.Fatal(err)
	}
	s := svc.NewSetup(u)

	tests := []struct {
		name, secret, code, password, field string
	}{
		{"wrong code", s.Secret, "000000", apptest.Password, "code"},
		{"not a secret", "not base32!", codeFor(t, s.Secret, 0), apptest.Password, "code"},
		{"wrong password", s.Secret, codeFor(t, s.Secret, 0), "not the password", "password"},
	}
	for _, tt := range tests {
		_, err := svc.Enable(t.Context(), u, tt.secret, tt.code, tt.password)
		if errs, ok := errors.AsType[validate.Errors](err); !ok || errs[tt.field] == "" {
			t.Errorf("%s: err = %v, want a message for %s", tt.name, err, tt.field)
		}
	}

	// Someone whose address isn't confirmed can't turn it on: whoever made
	// the account may not own the address, and would lock its owner out.
	eve, err := a.Accounts.CreateUser(t.Context(), "eve@example.com", apptest.Password, auth.RoleUser, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Enable(t.Context(), eve, s.Secret, codeFor(t, s.Secret, 0), apptest.Password)
	if errs, ok := errors.AsType[validate.Errors](err); !ok || errs["code"] == "" {
		t.Errorf("unconfirmed address: err = %v, want a message for code", err)
	}

	codes, err := svc.Enable(t.Context(), u, s.Secret, codeFor(t, s.Secret, 0), apptest.Password)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if len(codes) != 10 || !recoveryCodePattern.MatchString(codes[0]) {
		t.Errorf("recovery codes = %q", codes)
	}
	if st, err := svc.Status(t.Context(), u); err != nil || !st.On || st.RecoveryCodesLeft != 10 {
		t.Errorf("status = %+v, %v", st, err)
	}
	if _, err := svc.Enable(t.Context(), u, s.Secret, codeFor(t, s.Secret, 1), apptest.Password); !errors.Is(err, ErrAlreadyOn) {
		t.Errorf("enable twice: err = %v, want ErrAlreadyOn", err)
	}
	if sent := a.SentTo("ada@example.com"); len(sent) != 1 || sent[0].Subject != "Two-factor login is on" {
		t.Errorf("emails = %+v", sent)
	}

	// The stored secret is encrypted, and only the key it was stored with
	// opens it.
	row, err := store.New(a.DB.Read).GetTwoFactor(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := b32.DecodeString(s.Secret)
	if bytes.Contains(row.Secret, raw) {
		t.Error("the secret is stored unencrypted")
	}
	other, err := NewService(a.DB, apptest.New(t).Accounts, a.Logger, Options{AppName: "Test", Key: bytes.Repeat([]byte{1}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.CheckCode(t.Context(), u.ID, codeFor(t, s.Secret, 1)); err == nil {
		t.Error("a different key opened the secret")
	}
}

func TestLoginWithCode(t *testing.T) {
	t.Parallel()
	a, svc := newApp(t)
	u, err := a.Accounts.CreateUser(t.Context(), "ada@example.com", apptest.Password, auth.RoleUser, true)
	if err != nil {
		t.Fatal(err)
	}
	finish := func(challenge, code string) error {
		_, err := a.Accounts.FinishLogin(t.Context(), challenge, code, client, auth.BrowserSession)
		return err
	}

	// Without two-factor login, the password is enough.
	if _, err := a.Accounts.Login(t.Context(), u.Email, apptest.Password, client, auth.BrowserSession); err != nil {
		t.Fatalf("login: %v", err)
	}

	secret, codes := enable(t, svc, u)
	c := challenge(t, a, u)
	if id, err := svc.ChallengeUser(c); err != nil || id != u.ID {
		t.Errorf("ChallengeUser = %v, %v; want %v", id, err, u.ID)
	}
	if err := finish(c, "000000"); !errors.Is(err, auth.ErrInvalidCode) {
		t.Errorf("wrong code: err = %v, want ErrInvalidCode", err)
	}
	next := codeFor(t, secret, 1)
	if err := finish(c, next[:3]+" "+next[3:]); err != nil {
		t.Fatalf("right code: %v", err)
	}
	if err := finish(challenge(t, a, u), next); !errors.Is(err, auth.ErrInvalidCode) {
		t.Errorf("the same code again: err = %v, want ErrInvalidCode", err)
	}

	// A recovery code works once, typed any way.
	if err := finish(challenge(t, a, u), " "+strings.ToLower(codes[0])+" "); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	if err := finish(challenge(t, a, u), codes[0]); !errors.Is(err, auth.ErrInvalidCode) {
		t.Errorf("a used recovery code: err = %v, want ErrInvalidCode", err)
	}
	if st, _ := svc.Status(t.Context(), u); st.RecoveryCodesLeft != 9 {
		t.Errorf("recovery codes left = %d, want 9", st.RecoveryCodesLeft)
	}
	sent := a.SentTo(u.Email)
	if last := sent[len(sent)-1]; last.Subject != "A recovery code was used to log in" || !strings.Contains(last.Body, "You have 9 left") {
		t.Errorf("latest email: %s\n%s", last.Subject, last.Body)
	}

	// Challenges expire, and can't be forged or altered.
	c = challenge(t, a, u)
	svc.now = func() time.Time { return time.Now().Add(challengeTTL) }
	if err := finish(c, codes[1]); !errors.Is(err, auth.ErrLoginExpired) {
		t.Errorf("old challenge: err = %v, want ErrLoginExpired", err)
	}
	svc.now = time.Now
	for _, forged := range []string{"", "nonsense", c[:len(c)-2] + "AA"} {
		if err := finish(forged, codes[1]); !errors.Is(err, auth.ErrLoginExpired) {
			t.Errorf("challenge %q: err = %v, want ErrLoginExpired", forged, err)
		}
	}

	// Wrong codes are limited per account, whatever the challenge.
	for range 4 {
		_ = finish(challenge(t, a, u), "000000")
	}
	_, limited := errors.AsType[*auth.RateLimitedError](finish(challenge(t, a, u), codes[1]))
	if !limited {
		t.Error("the sixth code in 15 minutes wasn't limited")
	}
}

func TestRecoveryCodesAndTurningOff(t *testing.T) {
	t.Parallel()
	a, svc := newApp(t)
	u, err := a.Accounts.CreateUser(t.Context(), "ada@example.com", apptest.Password, auth.RoleUser, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NewRecoveryCodes(t.Context(), u, apptest.Password); !errors.Is(err, ErrNotOn) {
		t.Errorf("new codes while off: err = %v, want ErrNotOn", err)
	}
	_, old := enable(t, svc, u)

	if _, err := svc.NewRecoveryCodes(t.Context(), u, "not the password"); err == nil {
		t.Error("new codes with a wrong password succeeded")
	}
	codes, err := svc.NewRecoveryCodes(t.Context(), u, apptest.Password)
	if err != nil || len(codes) != 10 {
		t.Fatalf("new codes: %q, %v", codes, err)
	}
	if ok, _ := svc.CheckCode(t.Context(), u.ID, old[0]); ok {
		t.Error("an old recovery code still works")
	}

	if err := svc.Disable(t.Context(), u, "not the password"); err == nil {
		t.Error("turning off with a wrong password succeeded")
	}
	if err := svc.Disable(t.Context(), u, apptest.Password); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if st, _ := svc.Status(t.Context(), u); st.On {
		t.Error("still on after Disable")
	}
	if _, err := a.Accounts.Login(t.Context(), u.Email, apptest.Password, client, auth.BrowserSession); err != nil {
		t.Errorf("login after turning off: %v", err)
	}

	enable(t, svc, u)
	if err := svc.Reset(t.Context(), u.ID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := svc.Reset(t.Context(), u.ID); !errors.Is(err, ErrNotOn) {
		t.Errorf("reset while off: err = %v, want ErrNotOn", err)
	}
	sent := a.SentTo(u.Email)
	if last := sent[len(sent)-1]; last.Subject != "Two-factor login is off" {
		t.Errorf("latest email: %s", last.Subject)
	}
}
