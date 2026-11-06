package social

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
)

// apiPrefix is where providers send people back to: /api/auth/social/google/callback.
const apiPrefix = "/api/auth/social/"

// API serves logging in with providers under /api/auth/social. The
// frontend sends the browser (a page load, not fetch) to
// /api/auth/social/{provider}?next=/path&time_zone=…; it comes back to next
// logged in, or with ?error=… saying why not.
type API struct {
	svc *Service
}

// NewAPI returns the social login endpoints' handlers.
func NewAPI(svc *Service) *API {
	return &API{svc: svc}
}

// Routes registers the social login endpoints on mux.
func (h *API) Routes(mux *http.ServeMux) {
	limit := func(f http.HandlerFunc) http.Handler {
		return h.svc.accounts.RateLimit(f)
	}
	mux.HandleFunc("GET /api/auth/social", h.providers)
	mux.Handle("GET /api/auth/social/{provider}", limit(h.begin))
	mux.Handle("GET /api/auth/social/{provider}/callback", limit(h.callback))
	mux.Handle("GET /api/auth/social/identities", auth.RequireUser(http.HandlerFunc(h.identities)))
	mux.Handle("DELETE /api/auth/social/identities/{provider}", auth.RequireUser(http.HandlerFunc(h.disconnect)))
}

type providerJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// providers lists the providers that are on, for the login page's buttons.
func (h *API) providers(w http.ResponseWriter, _ *http.Request) {
	out := []providerJSON{}
	for _, p := range h.svc.Providers() {
		out = append(out, providerJSON(p))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"providers": out})
}

func (h *API) begin(w http.ResponseWriter, r *http.Request) {
	if err := begin(w, r, h.svc, apiPrefix); err != nil {
		httpx.Error(w, r, http.StatusNotFound)
	}
}

// callback is where a provider sends people back: it connects the account
// for someone logged in, and logs anyone else in, then returns to next.
func (h *API) callback(w http.ResponseWriter, r *http.Request) {
	ctx, id := r.Context(), r.PathValue("provider")
	acct, f, err := finish(w, r, h.svc, apiPrefix)
	next := httpx.SafeRedirect(f.Get("next"), "/")
	if u, ok := auth.UserFrom(ctx); ok {
		if err == nil {
			err = h.svc.Connect(ctx, u, id, acct)
		}
		h.back(w, r, next, err)
		return
	}

	var userID uuid.UUID
	if err == nil {
		userID, err = h.svc.SignIn(ctx, id, acct, f.Get("time_zone"))
	}
	var token string
	if err == nil {
		token, err = h.svc.accounts.LogInAs(ctx, userID, auth.ClientOf(r), auth.BrowserSession, id)
	}
	// readysetgo:begin module=twofactor
	if step, ok := errors.AsType[*auth.CodeRequiredError](err); ok {
		// The frontend asks for the code and sends it with the challenge
		// to /api/auth/login. A fragment never reaches a server's logs.
		http.Redirect(w, r, next+"#challenge="+url.QueryEscape(step.Challenge), http.StatusSeeOther) //nolint:gosec // next is checked to be a path on this site
		return
	}
	// readysetgo:end
	if err == nil {
		auth.SetSessionCookie(w, token)
	}
	h.back(w, r, next, err)
}

// back returns the browser to next, with ?error=… if err isn't nil.
func (h *API) back(w http.ResponseWriter, r *http.Request, next string, err error) {
	if err != nil {
		code := "failed"
		switch {
		case errors.Is(err, ErrStale):
			code = "expired"
		case errors.Is(err, ErrCancelled):
			code = "cancelled"
		case errors.Is(err, ErrUnverifiedEmail):
			code = "unverified_email"
		case errors.Is(err, ErrLinkedElsewhere):
			code = "linked_elsewhere"
		case errors.Is(err, ErrAlreadyConnected):
			code = "already_connected"
		case errors.Is(err, auth.ErrInvitationNeeded):
			code = "invitation_needed"
		case errors.Is(err, auth.ErrInvalidCredentials):
			code = "disabled"
		default:
			h.svc.logger.ErrorContext(r.Context(), "log in with provider", "provider", r.PathValue("provider"), "err", err)
		}
		u, _ := url.Parse(next)
		q := u.Query()
		q.Set("error", code)
		u.RawQuery = q.Encode()
		next = u.String()
	}
	http.Redirect(w, r, next, http.StatusSeeOther) //nolint:gosec // next is checked to be a path on this site
}

// identities lists the provider accounts connected to the user.
func (h *API) identities(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	ids, err := h.svc.Identities(r.Context(), u)
	if err != nil {
		h.svc.logger.ErrorContext(r.Context(), "list identities", "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
		return
	}
	type identityJSON struct {
		Provider    string    `json:"provider"`
		Name        string    `json:"name"`
		Email       string    `json:"email"`
		ConnectedAt time.Time `json:"connected_at"`
	}
	out := make([]identityJSON, len(ids))
	for i, id := range ids {
		out[i] = identityJSON{Provider: id.Provider.ID, Name: id.Provider.Name, Email: id.Email, ConnectedAt: id.ConnectedAt}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"identities": out})
}

func (h *API) disconnect(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	err := h.svc.Disconnect(r.Context(), u, r.PathValue("provider"))
	switch {
	case errors.Is(err, ErrLastWayIn):
		httpx.ErrorMessage(w, r, http.StatusConflict, "That's your only way to log in: set a password first.")
	case err != nil:
		h.svc.logger.ErrorContext(r.Context(), "disconnect provider", "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
