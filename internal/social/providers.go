package social

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

// A provider is a service people can log in with. Adding one means its
// OAuth endpoints, the scopes that read someone's address, and reading
// their account; only add services that verify addresses, since accounts
// are matched by address.
type provider struct {
	id, name string
	oauth    oauth2.Config
	extra    []oauth2.AuthCodeOption // more parameters for its consent page
	apiURL   string                  // where account reads its API
	account  func(ctx context.Context, c *http.Client, apiURL string) (Account, error)
}

// Account is someone's account at a provider.
type Account struct {
	Subject       string // the provider's ID for it, which never changes
	Email         string
	EmailVerified bool // whether the provider has checked the address is theirs
	Name          string
}

// newProviders returns the providers opts has credentials for.
func newProviders(opts Options) []*provider {
	var ps []*provider
	if c := opts.Google; c.ClientID != "" {
		ps = append(ps, &provider{
			id: "google", name: "Google",
			oauth: oauth2.Config{
				ClientID: c.ClientID, ClientSecret: c.ClientSecret, Endpoint: endpoints.Google,
				Scopes: []string{"openid", "email", "profile"},
			},
			extra:   []oauth2.AuthCodeOption{oauth2.SetAuthURLParam("prompt", "select_account")},
			apiURL:  "https://openidconnect.googleapis.com",
			account: googleAccount,
		})
	}
	if c := opts.GitHub; c.ClientID != "" {
		ps = append(ps, &provider{
			id: "github", name: "GitHub",
			oauth: oauth2.Config{
				ClientID: c.ClientID, ClientSecret: c.ClientSecret, Endpoint: endpoints.GitHub,
				Scopes: []string{"read:user", "user:email"},
			},
			apiURL:  "https://api.github.com",
			account: githubAccount,
		})
	}
	return ps
}

// googleAccount reads the OpenID Connect user info: fetched straight from
// Google with the access token, it needs no ID token checks.
func googleAccount(ctx context.Context, c *http.Client, apiURL string) (Account, error) {
	var info struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := getJSON(ctx, c, apiURL+"/v1/userinfo", &info); err != nil {
		return Account{}, err
	}
	return Account{Subject: info.Sub, Email: info.Email, EmailVerified: info.EmailVerified, Name: info.Name}, nil
}

// githubAccount reads the user and their primary address, which GitHub says
// is verified or not.
func githubAccount(ctx context.Context, c *http.Client, apiURL string) (Account, error) {
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := getJSON(ctx, c, apiURL+"/user", &user); err != nil {
		return Account{}, err
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, c, apiURL+"/user/emails", &emails); err != nil {
		return Account{}, err
	}
	a := Account{Name: cmp.Or(user.Name, user.Login)}
	if user.ID != 0 {
		a.Subject = strconv.FormatInt(user.ID, 10)
	}
	for _, e := range emails {
		if e.Primary {
			a.Email, a.EmailVerified = e.Email, e.Verified
		}
	}
	return a, nil
}

// getJSON reads url's JSON into v with c.
func getJSON(ctx context.Context, c *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "readysetgo") // GitHub refuses requests without one
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return nil
}

var errNoSubject = errors.New("the provider gave no account ID")
