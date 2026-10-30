package auth

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ukharwa/kickstart/internal/httpx"
	"github.com/ukharwa/kickstart/internal/ratelimit"
	"github.com/ukharwa/kickstart/internal/web"
)

//go:embed templates
var templateFS embed.FS

// Web serves the account pages.
type Web struct {
	svc     *Service
	pages   *web.Pages
	limiter ratelimit.Limiter
}

// NewWeb returns the account pages' handlers.
func NewWeb(svc *Service, rd *web.Renderer, limiter ratelimit.Limiter) (*Web, error) {
	files, _ := fs.Sub(templateFS, "templates")
	pages, err := rd.Pages(files, "internal/auth/templates")
	if err != nil {
		return nil, err
	}
	return &Web{svc: svc, pages: pages, limiter: limiter}, nil
}

// Routes registers the account pages on mux.
func (h *Web) Routes(mux *http.ServeMux) {
	// One budget per IP address across every form that checks a password or
	// sends email.
	limit := func(f http.HandlerFunc) http.Handler {
		return ratelimit.PerIP(h.limiter, "auth", 20, time.Minute)(f)
	}
	mux.HandleFunc("GET /register", h.form("register.html"))
	mux.Handle("POST /register", limit(h.register))
	mux.HandleFunc("GET /login", h.loginPage)
	mux.Handle("POST /login", limit(h.login))
	mux.HandleFunc("POST /logout", h.logout)
	mux.HandleFunc("GET /forgot-password", h.form("forgot_password.html"))
	mux.Handle("POST /forgot-password", limit(h.forgotPassword))
	mux.HandleFunc("GET /reset-password", h.tokenForm("reset_password.html"))
	mux.Handle("POST /reset-password", limit(h.resetPassword))
	mux.HandleFunc("GET /verify-email", h.tokenForm("verify_email.html"))
	mux.Handle("POST /verify-email", limit(h.verifyEmail))
	mux.HandleFunc("GET /confirm-email", h.tokenForm("confirm_email.html"))
	mux.Handle("POST /confirm-email", limit(h.confirmEmail))
	mux.Handle("GET /account", RequireUser(http.HandlerFunc(h.accountPage)))
	mux.Handle("POST /account/resend-verification", RequireUser(limit(h.resendVerification)))
	mux.Handle("POST /account/password", RequireUser(http.HandlerFunc(h.changePassword)))
	mux.Handle("POST /account/email", RequireUser(http.HandlerFunc(h.changeEmail)))
	mux.Handle("POST /account/delete", RequireUser(http.HandlerFunc(h.deleteAccount)))
}

// redirectToLogin sends a logged-out page request to the login page, which
// returns to the page afterwards. It reports false for JSON requests, which
// get a 401 instead.
func redirectToLogin(w http.ResponseWriter, r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	http.Redirect(w, r, "/login?"+url.Values{"next": {r.URL.RequestURI()}}.Encode(), http.StatusSeeOther)
	return true
}

// formPage is the data for the single-form pages.
type formPage struct {
	Form  web.Form
	Error string // a message about the whole form
	Token string // for pages reached from an emailed link
	Next  string // where to go after logging in
}

func (h *Web) form(page string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.pages.Render(w, r, http.StatusOK, page, formPage{})
	}
}

// tokenForm shows a page reached from an emailed link. The token goes into
// the form, and only submitting it acts: opening the link changes nothing,
// so mail scanners that open links don't use them up.
func (h *Web) tokenForm(page string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.pages.Render(w, r, http.StatusOK, page, formPage{Token: r.URL.Query().Get("token")})
	}
}

// fail re-renders page for a user mistake, or responds 500 for anything
// else.
func (h *Web) fail(w http.ResponseWriter, r *http.Request, page string, data formPage, err error) {
	var status int
	switch {
	case errors.As(err, &data.Form.Errors):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, ErrInvalidCredentials):
		status, data.Error = http.StatusUnauthorized, "Your email or password is incorrect."
	case errors.Is(err, ErrInvalidToken):
		status, data.Error = http.StatusBadRequest, "This link is invalid or has expired."
	default:
		if limited, ok := errors.AsType[*RateLimitedError](err); ok {
			w.Header().Set("Retry-After", fmt.Sprint(int(limited.RetryAfter.Seconds())))
			status = http.StatusTooManyRequests
			data.Error = fmt.Sprintf("Too many attempts. Try again in %d minutes.", int(limited.RetryAfter.Minutes())+1)
			break
		}
		h.svc.logger.ErrorContext(r.Context(), "account page", "page", page, "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
		return
	}
	h.pages.Render(w, r, status, page, data)
}

func (h *Web) register(w http.ResponseWriter, r *http.Request) {
	form := web.Form{Values: postForm(r)}
	if err := h.svc.Register(r.Context(), form.Get("email"), form.Get("password")); err != nil {
		h.fail(w, r, "register.html", formPage{Form: form}, err)
		return
	}
	web.SetFlash(w, "Check your inbox for a link to confirm your email address. You can log in now.")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Web) loginPage(w http.ResponseWriter, r *http.Request) {
	h.pages.Render(w, r, http.StatusOK, "login.html", formPage{Next: r.URL.Query().Get("next")})
}

func (h *Web) login(w http.ResponseWriter, r *http.Request) {
	form := web.Form{Values: postForm(r)}
	remember := form.Get("remember") != ""
	life := BrowserSession
	if remember {
		life = RememberedSession
	}
	ip := httpx.ClientIP(r.Context()).String()
	token, err := h.svc.Login(r.Context(), form.Get("email"), form.Get("password"), ip, life)
	if err != nil {
		h.fail(w, r, "login.html", formPage{Form: form, Next: form.Get("next")}, err)
		return
	}
	setSessionCookie(w, token, life, remember)
	http.Redirect(w, r, web.SafeRedirect(form.Get("next"), "/"), http.StatusSeeOther)
}

func (h *Web) logout(w http.ResponseWriter, r *http.Request) {
	if token := requestToken(r); token != "" {
		if err := h.svc.Logout(r.Context(), token); err != nil {
			h.svc.logger.ErrorContext(r.Context(), "logout", "err", err)
		}
	}
	clearSessionCookie(w)
	web.SetFlash(w, "You're logged out.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Web) forgotPassword(w http.ResponseWriter, r *http.Request) {
	form := web.Form{Values: postForm(r)}
	if err := h.svc.RequestPasswordReset(r.Context(), form.Get("email")); err != nil {
		h.fail(w, r, "forgot_password.html", formPage{Form: form}, err)
		return
	}
	web.SetFlash(w, "If that address has an account, we've emailed it a link to reset the password.")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Web) resetPassword(w http.ResponseWriter, r *http.Request) {
	form := web.Form{Values: postForm(r)}
	if err := h.svc.ResetPassword(r.Context(), form.Get("token"), form.Get("password")); err != nil {
		h.fail(w, r, "reset_password.html", formPage{Form: form, Token: form.Get("token")}, err)
		return
	}
	web.SetFlash(w, "Your password is changed, and you've been logged out everywhere. Log in with the new one.")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Web) verifyEmail(w http.ResponseWriter, r *http.Request) {
	form := web.Form{Values: postForm(r)}
	if err := h.svc.VerifyEmail(r.Context(), form.Get("token")); err != nil {
		h.fail(w, r, "verify_email.html", formPage{Form: form}, err)
		return
	}
	web.SetFlash(w, "Your email address is confirmed.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Web) confirmEmail(w http.ResponseWriter, r *http.Request) {
	form := web.Form{Values: postForm(r)}
	if err := h.svc.ConfirmEmailChange(r.Context(), form.Get("token")); err != nil {
		h.fail(w, r, "confirm_email.html", formPage{Form: form}, err)
		return
	}
	web.SetFlash(w, "Your email address is changed.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// accountPage is the data for the account page, which holds several forms.
type accountPage struct {
	User     User
	Email    web.Form
	Password web.Form
	Delete   web.Form
}

func (h *Web) accountPage(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	h.pages.Render(w, r, http.StatusOK, "account.html", accountPage{User: u})
}

// accountFail re-renders the account page with err shown on form, one of
// page's forms.
func (h *Web) accountFail(w http.ResponseWriter, r *http.Request, page *accountPage, form *web.Form, err error) {
	if !errors.As(err, &form.Errors) {
		h.fail(w, r, "account.html", formPage{}, err)
		return
	}
	h.pages.Render(w, r, http.StatusUnprocessableEntity, "account.html", page)
}

func (h *Web) resendVerification(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	if err := h.svc.ResendVerification(r.Context(), u); err != nil {
		h.fail(w, r, "account.html", formPage{}, err)
		return
	}
	web.SetFlash(w, "We've sent a new confirmation link to "+u.Email+".")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (h *Web) changePassword(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	page := accountPage{User: u, Password: web.Form{Values: postForm(r)}}
	err := h.svc.ChangePassword(r.Context(), u, tokenFrom(r.Context()),
		page.Password.Get("current_password"), page.Password.Get("new_password"))
	if err != nil {
		h.accountFail(w, r, &page, &page.Password, err)
		return
	}
	web.SetFlash(w, "Your password is changed. Any other devices have been logged out.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (h *Web) changeEmail(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	page := accountPage{User: u, Email: web.Form{Values: postForm(r)}}
	if err := h.svc.RequestEmailChange(r.Context(), u, page.Email.Get("password"), page.Email.Get("email")); err != nil {
		h.accountFail(w, r, &page, &page.Email, err)
		return
	}
	web.SetFlash(w, "Check the new address's inbox for a link to confirm the change.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (h *Web) deleteAccount(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	page := accountPage{User: u, Delete: web.Form{Values: postForm(r)}}
	if err := h.svc.DeleteAccount(r.Context(), u, page.Delete.Get("password")); err != nil {
		h.accountFail(w, r, &page, &page.Delete, err)
		return
	}
	clearSessionCookie(w)
	web.SetFlash(w, "Your account is deleted.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// postForm returns the submitted form fields. A malformed or oversized body
// yields an empty form, which then fails validation like any empty form.
func postForm(r *http.Request) url.Values {
	_ = r.ParseForm()
	return r.PostForm
}
