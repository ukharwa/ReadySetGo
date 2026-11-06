package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/ratelimit"
)

// SessionCookie is the session cookie's name. The __Host- prefix makes
// browsers require Secure and Path=/ and refuse a Domain, so a subdomain
// can't set or overwrite it.
const SessionCookie = "__Host-session"

type contextKey struct{}

// current is what LoadUser stores for a logged-in request.
type current struct {
	user  User
	token string
}

// UserFrom returns the logged-in user, if there is one.
func UserFrom(ctx context.Context) (User, bool) {
	c, ok := ctx.Value(contextKey{}).(current)
	return c.user, ok
}

// SessionToken returns the token of the session LoadUser found, or "". A
// feature passes it to SignOutOtherDevices to keep the current one.
func SessionToken(ctx context.Context) string {
	c, _ := ctx.Value(contextKey{}).(current)
	return c.token
}

// requestToken returns the session token sent with r: the session cookie,
// readysetgo:begin ui=api
// or an "Authorization: Bearer" header from a non-browser client,
// readysetgo:end
// or "".
func requestToken(r *http.Request) string {
	// readysetgo:begin ui=api
	if token, ok := bearerToken(r); ok {
		return token
	}
	// readysetgo:end
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// LoadUser is middleware that finds the request's session and puts its user
// in the context for UserFrom. It never rejects a request; RequireUser does
// that.
func (s *Service) LoadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := requestToken(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		u, err := s.Authenticate(r.Context(), token)
		switch {
		case errors.Is(err, ErrUnauthenticated):
			clearSessionCookie(w)
		case err != nil:
			s.logger.ErrorContext(r.Context(), "authenticate", "err", err)
			httpx.Error(w, r, http.StatusInternalServerError)
			return
		default:
			r = r.WithContext(context.WithValue(r.Context(), contextKey{}, current{user: u, token: token}))
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimit is middleware for routes that check a password or send email.
// Each client IP gets 20 requests a minute across every such route, in
// every feature, so spreading attempts over routes gains nothing.
func (s *Service) RateLimit(next http.Handler) http.Handler {
	return ratelimit.PerIP(s.limiter, "auth", 20, time.Minute)(next)
}

// RequireUser is middleware that lets only logged-in requests through.
// readysetgo:begin ui=html
// Pages redirect to the login page, which returns to the page afterwards;
// readysetgo:end
// JSON requests get 401 Unauthorized.
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}
		// readysetgo:begin ui=html
		if redirectToLogin(w, r) {
			return
		}
		// readysetgo:end
		httpx.Error(w, r, http.StatusUnauthorized)
	})
}

// RequireVerified is middleware for features that need a confirmed email
// address. Use it inside RequireUser.
func RequireVerified(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, _ := UserFrom(r.Context()); !u.EmailVerified {
			httpx.ErrorMessage(w, r, http.StatusForbidden, "Confirm your email address to use this. Your account page can resend the link.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole is middleware that lets only users with role through. Use it
// inside RequireUser.
func RequireRole(role string) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if u, _ := UserFrom(r.Context()); u.Role != role {
				httpx.Error(w, r, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// setSessionCookie sends token in the session cookie. remember keeps it for
// life's maximum age; otherwise it ends when the browser closes.
func setSessionCookie(w http.ResponseWriter, token string, life Lifetime, remember bool) {
	c := &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}
	if remember {
		c.MaxAge = int(life.Max / time.Second)
	}
	http.SetCookie(w, c)
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}
