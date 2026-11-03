package twofactor

import (
	"errors"
	"net/http"
	"time"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/ratelimit"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// API serves the two-factor login settings under /api/auth/two-factor. The
// code step of logging in is auth's: /api/auth/login and /api/auth/token
// answer 202 with a challenge, and take it back with the code.
type API struct {
	svc     *Service
	limiter ratelimit.Limiter
}

// NewAPI returns the two-factor endpoints' handlers.
func NewAPI(svc *Service, limiter ratelimit.Limiter) *API {
	return &API{svc: svc, limiter: limiter}
}

// Routes registers the two-factor endpoints on mux.
func (h *API) Routes(mux *http.ServeMux) {
	// Each change checks a password, so they share the account endpoints'
	// budget per IP address.
	limit := func(f http.HandlerFunc) http.Handler {
		return auth.RequireUser(ratelimit.PerIP(h.limiter, "auth", 20, time.Minute)(f))
	}
	mux.Handle("GET /api/auth/two-factor", auth.RequireUser(http.HandlerFunc(h.status)))
	mux.Handle("GET /api/auth/two-factor/setup", auth.RequireUser(http.HandlerFunc(h.setup)))
	mux.Handle("POST /api/auth/two-factor/enable", limit(h.enable))
	mux.Handle("POST /api/auth/two-factor/recovery-codes", limit(h.newRecoveryCodes))
	mux.Handle("POST /api/auth/two-factor/disable", limit(h.disable))
}

func (h *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var errs validate.Errors
	switch {
	case errors.As(err, &errs):
		httpx.WriteInvalid(w, r, errs)
	case errors.Is(err, ErrAlreadyOn):
		httpx.ErrorMessage(w, r, http.StatusConflict, "Two-factor login is already on.")
	case errors.Is(err, ErrNotOn):
		httpx.ErrorMessage(w, r, http.StatusConflict, "Two-factor login is off.")
	default:
		h.svc.logger.ErrorContext(r.Context(), "two-factor endpoint", "path", r.URL.Path, "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
	}
}

// status says whether two-factor login is on.
func (h *API) status(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	st, err := h.svc.Status(r.Context(), u)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := map[string]any{"enabled": st.On, "enabled_at": nil, "recovery_codes_left": st.RecoveryCodesLeft}
	if st.On {
		out["enabled_at"] = st.Since
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// setup returns a new secret. The frontend shows uri as a QR code, and secret
// for typing in, then sends the secret back to enable with a code.
func (h *API) setup(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	s := h.svc.NewSetup(u)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"secret": s.Secret, "uri": s.URI})
}

// enable turns two-factor login on and returns the recovery codes, which
// the frontend shows once.
func (h *API) enable(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Secret   string `json:"secret"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	u, _ := auth.UserFrom(r.Context())
	codes, err := h.svc.Enable(r.Context(), u, in.Secret, in.Code, in.Password)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

type password struct {
	Password string `json:"password"`
}

func (h *API) newRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var in password
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	u, _ := auth.UserFrom(r.Context())
	codes, err := h.svc.NewRecoveryCodes(r.Context(), u, in.Password)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (h *API) disable(w http.ResponseWriter, r *http.Request) {
	var in password
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	u, _ := auth.UserFrom(r.Context())
	if err := h.svc.Disable(r.Context(), u, in.Password); err != nil && !errors.Is(err, ErrNotOn) {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
