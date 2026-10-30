package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/kickstart/internal/httpx"
	"github.com/ukharwa/kickstart/internal/ratelimit"
	"github.com/ukharwa/kickstart/internal/validate"
)

// API serves the account endpoints as JSON under /api/auth/.
//
// A browser frontend on the same domain logs in with /api/auth/login and is
// then identified by the session cookie. Other clients (mobile apps, CLIs)
// get a bearer token from /api/auth/token and send it as
// "Authorization: Bearer <token>".
type API struct {
	svc     *Service
	limiter ratelimit.Limiter
}

// NewAPI returns the account endpoints' handlers.
func NewAPI(svc *Service, limiter ratelimit.Limiter) *API {
	return &API{svc: svc, limiter: limiter}
}

// Routes registers the account endpoints on mux.
func (h *API) Routes(mux *http.ServeMux) {
	// One budget per IP address across every endpoint that checks a password
	// or sends email.
	limit := func(f http.HandlerFunc) http.Handler {
		return ratelimit.PerIP(h.limiter, "auth", 20, time.Minute)(f)
	}
	mux.Handle("POST /api/auth/register", limit(h.register))
	mux.Handle("POST /api/auth/login", limit(h.login))
	mux.Handle("POST /api/auth/token", limit(h.token))
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.Handle("GET /api/auth/me", RequireUser(http.HandlerFunc(h.me)))
	mux.Handle("POST /api/auth/verify-email", limit(h.verifyEmail))
	mux.Handle("POST /api/auth/resend-verification", RequireUser(limit(h.resendVerification)))
	mux.Handle("POST /api/auth/forgot-password", limit(h.forgotPassword))
	mux.Handle("POST /api/auth/reset-password", limit(h.resetPassword))
	mux.Handle("POST /api/auth/change-password", RequireUser(http.HandlerFunc(h.changePassword)))
	mux.Handle("POST /api/auth/change-email", RequireUser(http.HandlerFunc(h.changeEmail)))
	mux.Handle("POST /api/auth/confirm-email", limit(h.confirmEmail))
	mux.Handle("POST /api/auth/delete-account", RequireUser(http.HandlerFunc(h.deleteAccount)))
}

// bearerToken returns the token from an "Authorization: Bearer" header.
func bearerToken(r *http.Request) (string, bool) {
	return strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// fail responds with the problem matching err.
func (h *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var errs validate.Errors
	switch {
	case errors.As(err, &errs):
		httpx.WriteInvalid(w, r, errs)
	case errors.Is(err, ErrInvalidCredentials):
		httpx.ErrorMessage(w, r, http.StatusUnauthorized, "Your email or password is incorrect.")
	case errors.Is(err, ErrInvalidToken):
		httpx.ErrorMessage(w, r, http.StatusBadRequest, "This link is invalid or has expired.")
	default:
		if limited, ok := errors.AsType[*RateLimitedError](err); ok {
			w.Header().Set("Retry-After", fmt.Sprint(int(limited.RetryAfter.Seconds())))
			httpx.ErrorMessage(w, r, http.StatusTooManyRequests, "Too many attempts. Try again later.")
			return
		}
		h.svc.logger.ErrorContext(r.Context(), "account endpoint", "path", r.URL.Path, "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
	}
}

// accepted confirms a request whose outcome is an email, without saying
// whether one was sent.
func accepted(w http.ResponseWriter, message string) {
	httpx.WriteJSON(w, http.StatusAccepted, map[string]string{"message": message})
}

func (h *API) register(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	if err := h.svc.Register(r.Context(), in.Email, in.Password); err != nil {
		h.fail(w, r, err)
		return
	}
	accepted(w, "Check your inbox for a link to confirm your email address. You can log in now.")
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

// login starts a session for a browser frontend, in the session cookie.
func (h *API) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	life := BrowserSession
	if in.Remember {
		life = RememberedSession
	}
	token, err := h.svc.Login(r.Context(), in.Email, in.Password, httpx.ClientIP(r.Context()).String(), life)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	setSessionCookie(w, token, life, in.Remember)
	w.WriteHeader(http.StatusNoContent)
}

// token starts a session for a non-browser client and returns its bearer
// token.
func (h *API) token(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	token, err := h.svc.Login(r.Context(), in.Email, in.Password, httpx.ClientIP(r.Context()).String(), APIToken)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token": token, "token_type": "Bearer", "expires_at": h.svc.now().Add(APIToken.Max),
	})
}

func (h *API) logout(w http.ResponseWriter, r *http.Request) {
	if token := requestToken(r); token != "" {
		if err := h.svc.Logout(r.Context(), token); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

type userJSON struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

func (h *API) me(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	httpx.WriteJSON(w, http.StatusOK, userJSON(u))
}

func (h *API) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	if err := h.svc.VerifyEmail(r.Context(), in.Token); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *API) resendVerification(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	if err := h.svc.ResendVerification(r.Context(), u); err != nil {
		h.fail(w, r, err)
		return
	}
	accepted(w, "We've sent a new confirmation link.")
}

func (h *API) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	if err := h.svc.RequestPasswordReset(r.Context(), in.Email); err != nil {
		h.fail(w, r, err)
		return
	}
	accepted(w, "If that address has an account, we've emailed it a link to reset the password.")
}

func (h *API) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token, Password string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	if err := h.svc.ResetPassword(r.Context(), in.Token, in.Password); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	u, _ := UserFrom(r.Context())
	if err := h.svc.ChangePassword(r.Context(), u, tokenFrom(r.Context()), in.CurrentPassword, in.NewPassword); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *API) changeEmail(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	u, _ := UserFrom(r.Context())
	if err := h.svc.RequestEmailChange(r.Context(), u, in.Password, in.Email); err != nil {
		h.fail(w, r, err)
		return
	}
	accepted(w, "Check the new address's inbox for a link to confirm the change.")
}

func (h *API) confirmEmail(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	if err := h.svc.ConfirmEmailChange(r.Context(), in.Token); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *API) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	u, _ := UserFrom(r.Context())
	if err := h.svc.DeleteAccount(r.Context(), u, in.Password); err != nil {
		h.fail(w, r, err)
		return
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
