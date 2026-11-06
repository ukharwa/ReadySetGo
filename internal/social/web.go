package social

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/web"
)

//go:embed templates
var templateFS embed.FS

// webPrefix is where providers send people back to the pages:
// /auth/google/callback.
const webPrefix = "/auth/"

// Web serves logging in with providers (/auth/{provider}, linked from the
// login and sign-up pages) and the page that connects and disconnects them.
type Web struct {
	svc   *Service
	pages *web.Pages
}

// NewWeb returns the social login pages' handlers.
func NewWeb(svc *Service, rd *web.Renderer) (*Web, error) {
	files, _ := fs.Sub(templateFS, "templates")
	pages, err := rd.Pages(files, "internal/social/templates")
	if err != nil {
		return nil, err
	}
	return &Web{svc: svc, pages: pages}, nil
}

// Routes registers the social login pages on mux.
func (h *Web) Routes(mux *http.ServeMux) {
	limit := func(f http.HandlerFunc) http.Handler {
		return h.svc.accounts.RateLimit(f)
	}
	mux.Handle("GET /auth/{provider}", limit(h.begin))
	mux.Handle("GET /auth/{provider}/callback", limit(h.callback))
	mux.Handle("GET /account/sign-in", auth.RequireUser(http.HandlerFunc(h.show)))
	mux.Handle("POST /account/sign-in/{provider}/disconnect", auth.RequireUser(http.HandlerFunc(h.disconnect)))
}

func (h *Web) begin(w http.ResponseWriter, r *http.Request) {
	if err := begin(w, r, h.svc, webPrefix); err != nil {
		httpx.Error(w, r, http.StatusNotFound)
	}
}

// callback is where a provider sends people back: it connects the account
// for someone logged in, and logs anyone else in.
func (h *Web) callback(w http.ResponseWriter, r *http.Request) {
	ctx, id := r.Context(), r.PathValue("provider")
	acct, f, err := finish(w, r, h.svc, webPrefix)
	if u, ok := auth.UserFrom(ctx); ok {
		if err == nil {
			err = h.svc.Connect(ctx, u, id, acct)
		}
		if err != nil {
			h.failed(w, r, "/account/sign-in", err)
			return
		}
		web.SetFlash(w, "Connected.")
		http.Redirect(w, r, "/account/sign-in", http.StatusSeeOther)
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
		h.pages.Render(w, r, http.StatusOK, "login_code.html", codePage{Challenge: step.Challenge, Next: f.Get("next")})
		return
	}
	// readysetgo:end
	if err != nil {
		h.failed(w, r, "/login", err)
		return
	}
	auth.SetSessionCookie(w, token)
	http.Redirect(w, r, web.SafeRedirect(f.Get("next"), "/"), http.StatusSeeOther) //nolint:gosec // SafeRedirect keeps it on this site
}

// readysetgo:begin module=twofactor
// codePage asks for a two-factor code after a provider: its form goes to
// the login page's code step.
type codePage struct {
	Challenge, Next string
}

// readysetgo:end
// failed tells the person what went wrong, on the page at to.
func (h *Web) failed(w http.ResponseWriter, r *http.Request, to string, err error) {
	message := "Something went wrong there. Try again."
	switch {
	case errors.Is(err, ErrStale), errors.Is(err, ErrCancelled), errors.Is(err, ErrUnverifiedEmail),
		errors.Is(err, ErrLinkedElsewhere), errors.Is(err, ErrAlreadyConnected):
		message = sentence(err)
	case errors.Is(err, auth.ErrInvitationNeeded):
		message = "Signing up needs an invitation."
	case errors.Is(err, auth.ErrInvalidCredentials):
		message = "That account can't log in."
	default:
		h.svc.logger.ErrorContext(r.Context(), "log in with provider", "provider", r.PathValue("provider"), "err", err)
	}
	web.SetFlash(w, message)
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// sentence turns an error message into a sentence to show.
func sentence(err error) string {
	s := err.Error()
	return strings.ToUpper(s[:1]) + s[1:] + "."
}

// signInPage lists the ways someone can log in.
type signInPage struct {
	HasPassword bool
	Identities  []Identity
	Unconnected []Provider // providers that are on and not connected
}

func (h *Web) show(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	ids, err := h.svc.Identities(r.Context(), u)
	if err != nil {
		h.svc.logger.ErrorContext(r.Context(), "ways to log in page", "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
		return
	}
	page := signInPage{HasPassword: u.HasPassword, Identities: ids}
	for _, p := range h.svc.Providers() {
		connected := false
		for _, id := range ids {
			connected = connected || id.Provider.ID == p.ID
		}
		if !connected {
			page.Unconnected = append(page.Unconnected, p)
		}
	}
	h.pages.Render(w, r, http.StatusOK, "sign_in.html", page)
}

func (h *Web) disconnect(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	err := h.svc.Disconnect(r.Context(), u, r.PathValue("provider"))
	switch {
	case errors.Is(err, ErrLastWayIn):
		web.SetFlash(w, sentence(err))
	case err != nil:
		h.svc.logger.ErrorContext(r.Context(), "disconnect provider", "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
		return
	default:
		web.SetFlash(w, "Disconnected.")
	}
	http.Redirect(w, r, "/account/sign-in", http.StatusSeeOther)
}
