package apptest

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/web"
)

// Renderer returns a page renderer using the embedded templates.
func (a *App) Renderer() *web.Renderer {
	a.t.Helper()
	rd, err := web.New(web.Options{
		AppName:    "Test",
		OpenSignup: !a.Accounts.InviteOnly(),
		User: func(ctx context.Context) any {
			if u, ok := auth.UserFrom(ctx); ok {
				return &u
			}
			return nil
		},
		TimeZone: func(ctx context.Context) *time.Location {
			if u, ok := auth.UserFrom(ctx); ok {
				return u.Location()
			}
			return nil
		},
		Logger: a.Logger,
	})
	if err != nil {
		a.t.Fatal(err)
	}
	return rd
}

// Page is a response, with its body read.
type Page struct {
	Status   int
	Location string // where a redirect points
	Header   http.Header
	Body     string
}

// Get fetches path with c.
func (a *App) Get(c *http.Client, path string) Page {
	a.t.Helper()
	req, err := http.NewRequestWithContext(a.t.Context(), http.MethodGet, a.URL(path), nil)
	if err != nil {
		a.t.Fatal(err)
	}
	return a.do(c, req)
}

// Post submits a form to path with c, as a browser on the same site would.
func (a *App) Post(c *http.Client, path string, form url.Values) Page {
	a.t.Helper()
	req, err := http.NewRequestWithContext(a.t.Context(), http.MethodPost, a.URL(path), strings.NewReader(form.Encode()))
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	return a.do(c, req)
}

func (a *App) do(c *http.Client, req *http.Request) Page {
	a.t.Helper()
	resp, err := c.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		a.t.Fatal(err)
	}
	return Page{Status: resp.StatusCode, Location: resp.Header.Get("Location"), Header: resp.Header, Body: string(b)}
}
