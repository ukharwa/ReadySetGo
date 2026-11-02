// Package workspacestest helps test features whose data belongs to a
// workspace, above all that one workspace's data can't be reached from
// another.
package workspacestest

import (
	"net/http"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/workspaces"
)

// New starts an apptest.App with the workspaces service hooked into its
// accounts, as the app does.
func New(t testing.TB) (*apptest.App, *workspaces.Service) {
	t.Helper()
	a := apptest.New(t)
	svc, err := workspaces.NewService(a.DB, a.Accounts, a.Limiter, a.Logger, workspaces.Options{
		AppName: "Test", BaseURL: apptest.BaseURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, svc
}

// Person is someone signed in, with their personal workspace.
type Person struct {
	Client    *http.Client
	User      auth.User
	Workspace workspaces.Workspace
}

// SignIn creates a person with address, logged in on their own client. To
// check isolation, sign in two people and have one request the other's
// workspace URLs: every one must answer 404.
func SignIn(t testing.TB, a *apptest.App, svc *workspaces.Service, address string) Person {
	t.Helper()
	c := a.Client()
	u := a.SignIn(c, address)
	all, err := svc.ForUser(t.Context(), u)
	if err != nil || len(all) != 1 {
		t.Fatalf("%s's workspaces: %v (err %v), want one personal workspace", address, all, err)
	}
	return Person{Client: c, User: u, Workspace: all[0]}
}
