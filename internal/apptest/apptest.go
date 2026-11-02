// Package apptest builds a whole application for end-to-end tests: a fresh
// database, the account service, the real middleware stack behind an HTTPS
// test server, and a job worker that delivers email to a recorder. A test
// registers the routes it exercises on Mux and talks to Server like a
// browser or API client would.
package apptest

import (
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/db/dbtest"
	"github.com/ukharwa/readysetgo/internal/email"
	"github.com/ukharwa/readysetgo/internal/email/emailtest"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/jobs"
	"github.com/ukharwa/readysetgo/internal/ratelimit"
)

// BaseURL is the address emailed links point at in tests.
const BaseURL = "https://app.test"

// Password is the password of users made by SignIn.
const Password = "correct horse battery"

// App is a running test application.
type App struct {
	DB       *db.DB
	Accounts *auth.Service
	Limiter  *ratelimit.Memory
	Emails   *emailtest.Recorder
	Worker   *jobs.Worker
	Mux      *http.ServeMux // register the routes under test here
	Server   *httptest.Server
	Logger   *slog.Logger
	t        testing.TB
}

// New starts an App. Everything is cleaned up when the test ends.
func New(t testing.TB) *App {
	t.Helper()
	d := dbtest.New(t)
	logger := slog.New(slog.DiscardHandler)
	limiter := ratelimit.NewMemory()
	accounts, err := auth.NewService(d, auth.DBSessions{DB: d}, limiter, logger, auth.Options{
		AppName: "Test", BaseURL: BaseURL, MinPasswordLength: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	emails := &emailtest.Recorder{}
	worker := jobs.NewWorker(d, logger)
	worker.Handle(email.JobKind, email.Handler(emails))

	mux := http.NewServeMux()
	stack := append(httpx.Stack(logger, nil, true), accounts.LoadUser)
	server := httptest.NewTLSServer(httpx.Chain(mux, stack...))
	t.Cleanup(server.Close)

	return &App{
		DB: d, Accounts: accounts, Limiter: limiter, Emails: emails, Worker: worker,
		Mux: mux, Server: server, Logger: logger, t: t,
	}
}

// Client returns a client with its own cookie jar, like a separate browser.
// It doesn't follow redirects, so tests can check where they point.
func (a *App) Client() *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		a.t.Fatal(err)
	}
	c := *a.Server.Client()
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// SignIn creates a user with a confirmed address (running the account
// hooks, as signing up would) and logs c in as them.
func (a *App) SignIn(c *http.Client, address string) auth.User {
	a.t.Helper()
	u, err := a.Accounts.CreateUser(a.t.Context(), address, Password, auth.RoleUser, true)
	if err != nil {
		a.t.Fatalf("create %s: %v", address, err)
	}
	token, err := a.Accounts.Login(a.t.Context(), address, Password, "192.0.2.1", auth.BrowserSession)
	if err != nil {
		a.t.Fatalf("log in %s: %v", address, err)
	}
	server, _ := url.Parse(a.Server.URL)
	c.Jar.SetCookies(server, []*http.Cookie{{
		Name: auth.SessionCookie, Value: token, Path: "/",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}})
	return u
}

// SentTo runs the queued jobs and returns the emails sent to address so
// far, oldest first.
func (a *App) SentTo(address string) []email.Message {
	a.t.Helper()
	for {
		ran, err := a.Worker.RunOne(a.t.Context())
		if err != nil {
			a.t.Fatal(err)
		}
		if !ran {
			break
		}
	}
	var out []email.Message
	for _, m := range a.Emails.Messages() {
		if m.To == address {
			out = append(out, m)
		}
	}
	return out
}

var linkPattern = regexp.MustCompile(`https?://\S+`)

// TokenFrom returns the token in the link to path in the latest email sent
// to address, failing the test if there is none.
func (a *App) TokenFrom(address, path string) string {
	a.t.Helper()
	sent := a.SentTo(address)
	if len(sent) == 0 {
		a.t.Fatalf("no email sent to %s", address)
	}
	for _, link := range linkPattern.FindAllString(sent[len(sent)-1].Body, -1) {
		u, err := url.Parse(link)
		if err == nil && u.Path == path && u.Query().Get("token") != "" {
			return u.Query().Get("token")
		}
	}
	a.t.Fatalf("latest email to %s has no %s link:\n%s", address, path, sent[len(sent)-1].Body)
	return ""
}

// URL returns the server's address for path.
func (a *App) URL(path string) string {
	return a.Server.URL + path
}

// Contains reports whether every one of subs is in s; tests use it to check
// pages and messages.
func Contains(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
