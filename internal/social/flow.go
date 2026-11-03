package social

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"
)

// The browser's round trip to a provider, shared by the pages and the API.

const (
	flowCookie = "__Host-social"
	flowTTL    = 10 * time.Minute
)

// Errors from a provider's callback.
var (
	ErrStale     = errors.New("that took too long, or was started in another browser: try again")
	ErrCancelled = errors.New("logging in was cancelled")
)

// callbackURL is where provider id sends people back to, under prefix.
func (s *Service) callbackURL(prefix, id string) string {
	return s.opts.BaseURL + prefix + id + "/callback"
}

// begin sends the browser to the consent page of the provider in r's path,
// keeping a random state, PKCE's verifier, and r's next and time_zone query
// parameters in a short-lived cookie for finish. It's a GET, from a link:
// pages' Content-Security-Policy (form-action 'self') would stop a form
// redirecting to the provider.
func begin(w http.ResponseWriter, r *http.Request, s *Service, prefix string) error {
	id := r.PathValue("provider")
	f := url.Values{
		"provider": {id}, "state": {rand.Text()}, "verifier": {oauth2.GenerateVerifier()},
		"next": {r.URL.Query().Get("next")}, "time_zone": {r.URL.Query().Get("time_zone")},
	}
	to, err := s.authURL(id, s.callbackURL(prefix, id), f.Get("state"), f.Get("verifier"))
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: flowCookie, Value: base64.RawURLEncoding.EncodeToString([]byte(f.Encode())), Path: "/",
		MaxAge: int(flowTTL / time.Second), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, to, http.StatusSeeOther) //nolint:gosec // the provider's consent page, from its configuration
	return nil
}

// finish reads a provider's callback. It checks the state against begin's
// cookie, which it clears, and returns the person's account at the provider
// with what begin kept (next and time_zone).
func finish(w http.ResponseWriter, r *http.Request, s *Service, prefix string) (Account, url.Values, error) {
	http.SetCookie(w, &http.Cookie{
		Name: flowCookie, Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	f := url.Values{}
	if c, err := r.Cookie(flowCookie); err == nil {
		if b, err := base64.RawURLEncoding.DecodeString(c.Value); err == nil {
			f, _ = url.ParseQuery(string(b))
		}
	}
	id, q := r.PathValue("provider"), r.URL.Query()
	if f.Get("provider") != id || f.Get("state") == "" ||
		subtle.ConstantTimeCompare([]byte(f.Get("state")), []byte(q.Get("state"))) != 1 {
		return Account{}, f, ErrStale
	}
	if q.Get("error") != "" {
		return Account{}, f, ErrCancelled
	}
	acct, err := s.Exchange(r.Context(), id, s.callbackURL(prefix, id), q.Get("code"), f.Get("verifier"))
	return acct, f, err
}
