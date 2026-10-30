package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/db/dbtest"
	"github.com/ukharwa/kickstart/internal/ratelimit"
)

func TestHasher(t *testing.T) {
	t.Parallel()
	h := newHasher()
	ctx := context.Background()
	hash, err := h.Hash(ctx, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash %q isn't in the expected PHC format", hash)
	}
	if ok, rehash, err := h.Verify(ctx, hash, "correct horse battery"); !ok || rehash || err != nil {
		t.Errorf("right password: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	if ok, _, _ := h.Verify(ctx, hash, "wrong"); ok {
		t.Error("wrong password verified")
	}

	// A hash made with weaker settings still verifies and asks to be redone.
	weaker := strings.Replace(hash, "t=2", "t=1", 1)
	if _, rehash, err := h.Verify(ctx, weaker, "correct horse battery"); !rehash || err != nil {
		t.Errorf("old settings: rehash=%v err=%v", rehash, err)
	}
	if _, _, err := h.Verify(ctx, "$2a$12$bcrypt", "x"); err == nil {
		t.Error("malformed hash accepted")
	}
}

func newTestService(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	d := dbtest.New(t)
	s, err := NewService(d, DBSessions{DB: d}, ratelimit.NewMemory(), slog.New(slog.DiscardHandler),
		Options{AppName: "Test", BaseURL: "https://app.test", MinPasswordLength: 12})
	if err != nil {
		t.Fatal(err)
	}
	now := db.Now()
	s.now = func() time.Time { return now }
	return s, &now
}

// TestSessionExpiry walks a browser session through its idle and absolute
// limits.
func TestSessionExpiry(t *testing.T) {
	t.Parallel()
	s, now := newTestService(t)
	ctx := t.Context()
	if _, err := s.CreateUser(ctx, "ada@example.com", "correct horse battery", RoleUser, true); err != nil {
		t.Fatal(err)
	}
	token, err := s.Login(ctx, "ada@example.com", "correct horse battery", "192.0.2.1", BrowserSession)
	if err != nil {
		t.Fatal(err)
	}
	authenticated := func() bool {
		_, err := s.Authenticate(ctx, token)
		return err == nil
	}

	// Used every 20 hours, it stays alive past the 24-hour idle limit...
	for range 8 {
		*now = now.Add(20 * time.Hour)
		if !authenticated() {
			t.Fatalf("session expired while in use, at %v", *now)
		}
	}
	// ...until the 7-day maximum.
	*now = now.Add(9 * time.Hour) // 169 hours in all
	if authenticated() {
		t.Error("session outlived its 7-day maximum")
	}

	token, _ = s.Login(ctx, "ada@example.com", "correct horse battery", "192.0.2.1", BrowserSession)
	*now = now.Add(25 * time.Hour)
	if authenticated() {
		t.Error("session survived 25 idle hours")
	}
}

func TestDisabledUserLosesSessions(t *testing.T) {
	t.Parallel()
	s, _ := newTestService(t)
	ctx := t.Context()
	if _, err := s.CreateUser(ctx, "ada@example.com", "correct horse battery", RoleUser, true); err != nil {
		t.Fatal(err)
	}
	token, err := s.Login(ctx, "ada@example.com", "correct horse battery", "192.0.2.1", BrowserSession)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDisabled(ctx, "ada@example.com", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); err == nil {
		t.Error("disabled user still authenticated")
	}
	if _, err := s.Login(ctx, "ada@example.com", "correct horse battery", "192.0.2.1", BrowserSession); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("disabled user login: err = %v, want ErrInvalidCredentials", err)
	}
	if err := s.SetDisabled(ctx, "nobody@example.com", true); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user: err = %v", err)
	}
}

func TestCleanup(t *testing.T) {
	t.Parallel()
	s, now := newTestService(t)
	ctx := t.Context()
	if _, err := s.CreateUser(ctx, "ada@example.com", "correct horse battery", RoleUser, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "ada@example.com", "correct horse battery", "192.0.2.1", BrowserSession); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(8 * 24 * time.Hour)
	if err := s.Cleanup()(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d expired sessions left", n)
	}
}
