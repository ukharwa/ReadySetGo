package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/ratelimit"
	"github.com/ukharwa/readysetgo/internal/validate"
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
	mux.Handle("POST /api/auth/profile", RequireUser(http.HandlerFunc(h.updateProfile)))
	mux.Handle("POST /api/auth/verify-email", limit(h.verifyEmail))
	mux.Handle("POST /api/auth/resend-verification", RequireUser(limit(h.resendVerification)))
	mux.Handle("POST /api/auth/forgot-password", limit(h.forgotPassword))
	mux.Handle("POST /api/auth/reset-password", limit(h.resetPassword))
	mux.Handle("POST /api/auth/change-password", RequireUser(http.HandlerFunc(h.changePassword)))
	mux.Handle("POST /api/auth/change-email", RequireUser(http.HandlerFunc(h.changeEmail)))
	mux.Handle("POST /api/auth/confirm-email", limit(h.confirmEmail))
	mux.Handle("POST /api/auth/delete-account", RequireUser(http.HandlerFunc(h.deleteAccount)))
	mux.Handle("GET /api/auth/devices", RequireUser(http.HandlerFunc(h.devices)))
	mux.Handle("DELETE /api/auth/devices/{id}", RequireUser(http.HandlerFunc(h.signOutDevice)))
	mux.Handle("POST /api/auth/devices/sign-out-others", RequireUser(http.HandlerFunc(h.signOutOtherDevices)))
}

// bearerToken returns the token from an "Authorization: Bearer" header.
func bearerToken(r *http.Request) (string, bool) {
	return strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// fail responds with the problem matching err.
func (h *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	// readysetgo:begin module=twofactor
	if step, ok := errors.AsType[*CodeRequiredError](err); ok {
		// Not a failure: the password was right, and a code is needed too.
		// The client sends {"challenge", "code"} to the same endpoint.
		httpx.WriteJSON(w, http.StatusAccepted, map[string]string{
			"challenge": step.Challenge,
			"message":   "Enter the code from your authenticator app, or a recovery code.",
		})
		return
	}
	// readysetgo:end
	var errs validate.Errors
	switch {
	case errors.As(err, &errs):
		httpx.WriteInvalid(w, r, errs)
	case errors.Is(err, ErrInvalidCredentials):
		httpx.ErrorMessage(w, r, http.StatusUnauthorized, "Your email or password is incorrect.")
	case errors.Is(err, ErrInvalidToken):
		httpx.ErrorMessage(w, r, http.StatusBadRequest, "This link is invalid or has expired.")
	// readysetgo:begin module=twofactor
	case errors.Is(err, ErrInvalidCode):
		httpx.ErrorMessage(w, r, http.StatusUnauthorized, "That code isn't right.")
	case errors.Is(err, ErrLoginExpired):
		httpx.ErrorMessage(w, r, http.StatusUnauthorized, "That took too long. Log in again.")
	// readysetgo:end
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
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
		TimeZone string `json:"time_zone"`
		Token    string `json:"token"` // from an invitation link, when signing up through one
	}
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	reg := Registration{Email: in.Email, Password: in.Password, Name: in.Name, TimeZone: in.TimeZone, Invite: in.Token}
	if err := h.svc.Register(r.Context(), reg); err != nil {
		h.fail(w, r, err)
		return
	}
	accepted(w, h.svc.registeredMessage(reg))
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
	// readysetgo:begin module=twofactor
	Challenge string `json:"challenge"` // with code, in place of email and password: the second step
	Code      string `json:"code"`
	// readysetgo:end
}

// logIn starts a session with lifetime life from in: a password, or the
// challenge and code of a second step.
func (h *API) logIn(r *http.Request, in credentials, life Lifetime) (string, error) {
	// readysetgo:begin module=twofactor
	if in.Challenge != "" {
		return h.svc.FinishLogin(r.Context(), in.Challenge, in.Code, ClientOf(r), life)
	}
	// readysetgo:end
	return h.svc.Login(r.Context(), in.Email, in.Password, ClientOf(r), life)
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
	token, err := h.logIn(r, in, life)
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
	var in credentials
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	token, err := h.logIn(r, in, APIToken)
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
	Name          string    `json:"name"`
	TimeZone      string    `json:"time_zone"`
	Role          string    `json:"role"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	// readysetgo:begin module=social
	HasPassword bool `json:"has_password"`
	// readysetgo:end
}

func (h *API) me(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	httpx.WriteJSON(w, http.StatusOK, userJSON(u))
}

// updateProfile changes the user's name and time zone, and returns the
// updated user.
func (h *API) updateProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		TimeZone string `json:"time_zone"`
	}
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	u, _ := UserFrom(r.Context())
	u, err := h.svc.UpdateProfile(r.Context(), u, in.Name, in.TimeZone)
	if err != nil {
		h.fail(w, r, err)
		return
	}
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
	if err := h.svc.ChangePassword(r.Context(), u, SessionToken(r.Context()), in.CurrentPassword, in.NewPassword); err != nil {
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

type deviceJSON struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Current    bool      `json:"current"`
}

// devices lists the user's signed-in browsers and tokens.
func (h *API) devices(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	devices, err := h.svc.Devices(r.Context(), u, SessionToken(r.Context()))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]deviceJSON, len(devices))
	for i, d := range devices {
		out[i] = deviceJSON(d)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *API) signOutDevice(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	switch err := h.svc.SignOutDevice(r.Context(), u, id); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrDeviceNotFound):
		httpx.Error(w, r, http.StatusNotFound)
	default:
		h.fail(w, r, err)
	}
}

func (h *API) signOutOtherDevices(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	if err := h.svc.SignOutOtherDevices(r.Context(), u, SessionToken(r.Context())); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
