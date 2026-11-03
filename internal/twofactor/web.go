package twofactor

import (
	"embed"
	"encoding/base64"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"time"

	"rsc.io/qr"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/ratelimit"
	"github.com/ukharwa/readysetgo/internal/web"
)

//go:embed templates
var templateFS embed.FS

// Web serves the two-factor login settings page, linked from the account
// page. The code step of logging in is auth's login page.
type Web struct {
	svc     *Service
	pages   *web.Pages
	limiter ratelimit.Limiter
}

// NewWeb returns the two-factor pages' handlers.
func NewWeb(svc *Service, rd *web.Renderer, limiter ratelimit.Limiter) (*Web, error) {
	files, _ := fs.Sub(templateFS, "templates")
	pages, err := rd.Pages(files, "internal/twofactor/templates")
	if err != nil {
		return nil, err
	}
	return &Web{svc: svc, pages: pages, limiter: limiter}, nil
}

// Routes registers the two-factor pages on mux.
func (h *Web) Routes(mux *http.ServeMux) {
	// Each form checks a password, so they share the account pages' budget
	// per IP address.
	form := func(f http.HandlerFunc) http.Handler {
		return auth.RequireUser(ratelimit.PerIP(h.limiter, "auth", 20, time.Minute)(f))
	}
	mux.Handle("GET /account/two-factor", auth.RequireUser(http.HandlerFunc(h.show)))
	mux.Handle("POST /account/two-factor/enable", form(h.enable))
	mux.Handle("POST /account/two-factor/recovery-codes", form(h.newRecoveryCodes))
	mux.Handle("POST /account/two-factor/disable", form(h.disable))
}

// settingsPage is the data for the settings page, which holds several
// forms.
type settingsPage struct {
	Status  Status
	Setup   Setup        // when it's off: the secret to add to the app
	QR      template.URL // Setup.URI as a QR code image
	Enable  web.Form
	Codes   web.Form // making new recovery codes
	Disable web.Form
}

func (h *Web) show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, &settingsPage{})
}

// render shows the settings page. When two-factor login is off, it offers
// page.Setup's secret, or a new one.
func (h *Web) render(w http.ResponseWriter, r *http.Request, status int, page *settingsPage) {
	u, _ := auth.UserFrom(r.Context())
	var err error
	if page.Status, err = h.svc.Status(r.Context(), u); err != nil {
		h.serverError(w, r, err)
		return
	}
	if !page.Status.On {
		if page.Setup.Secret == "" {
			page.Setup = h.svc.NewSetup(u)
		}
		if page.QR, err = qrCode(page.Setup.URI); err != nil {
			h.serverError(w, r, err)
			return
		}
	}
	h.pages.Render(w, r, status, "two_factor.html", page)
}

// qrCode returns text as a QR code, in a data: URL for an <img>.
func qrCode(text string) (template.URL, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(c.PNG())), nil //nolint:gosec // a PNG made here, base64-encoded
}

func (h *Web) enable(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	form := web.Form{Values: postForm(r)}
	codes, err := h.svc.Enable(r.Context(), u, form.Get("secret"), form.Get("code"), form.Get("password"))
	switch {
	case errors.As(err, &form.Errors):
		page := &settingsPage{Enable: form}
		if _, err := b32.DecodeString(form.Get("secret")); err == nil {
			page.Setup = h.svc.setup(u, form.Get("secret")) // so the app needn't scan a new one
		}
		h.render(w, r, http.StatusUnprocessableEntity, page)
	case errors.Is(err, ErrAlreadyOn):
		http.Redirect(w, r, "/account/two-factor", http.StatusSeeOther)
	case err != nil:
		h.serverError(w, r, err)
	default:
		h.pages.Render(w, r, http.StatusOK, "recovery_codes.html", codes)
	}
}

func (h *Web) newRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	form := web.Form{Values: postForm(r)}
	codes, err := h.svc.NewRecoveryCodes(r.Context(), u, form.Get("password"))
	switch {
	case errors.As(err, &form.Errors):
		h.render(w, r, http.StatusUnprocessableEntity, &settingsPage{Codes: form})
	case errors.Is(err, ErrNotOn):
		http.Redirect(w, r, "/account/two-factor", http.StatusSeeOther)
	case err != nil:
		h.serverError(w, r, err)
	default:
		h.pages.Render(w, r, http.StatusOK, "recovery_codes.html", codes)
	}
}

func (h *Web) disable(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	form := web.Form{Values: postForm(r)}
	err := h.svc.Disable(r.Context(), u, form.Get("password"))
	switch {
	case errors.As(err, &form.Errors):
		h.render(w, r, http.StatusUnprocessableEntity, &settingsPage{Disable: form})
	case err != nil && !errors.Is(err, ErrNotOn):
		h.serverError(w, r, err)
	default:
		web.SetFlash(w, "Two-factor login is off.")
		http.Redirect(w, r, "/account/two-factor", http.StatusSeeOther)
	}
}

func (h *Web) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.svc.logger.ErrorContext(r.Context(), "two-factor page", "err", err)
	httpx.Error(w, r, http.StatusInternalServerError)
}

// postForm returns the submitted form fields; a malformed body gives an
// empty form, which fails validation.
func postForm(r *http.Request) url.Values {
	_ = r.ParseForm()
	return r.PostForm
}
