package admin

import (
	"cmp"
	"embed"
	"errors"
	"io/fs"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/validate"
	"github.com/ukharwa/readysetgo/internal/web"
)

//go:embed templates
var templateFS embed.FS

// Web serves the admin pages under /admin/.
type Web struct {
	svc   *Service
	pages *web.Pages
}

// NewWeb returns the admin pages' handlers.
func NewWeb(svc *Service, rd *web.Renderer) (*Web, error) {
	files, _ := fs.Sub(templateFS, "templates")
	pages, err := rd.Pages(files, "internal/admin/templates")
	if err != nil {
		return nil, err
	}
	return &Web{svc: svc, pages: pages}, nil
}

// Routes registers the admin pages on mux, for admins only.
func (h *Web) Routes(mux *http.ServeMux) {
	admin := func(f http.HandlerFunc) http.Handler {
		return auth.RequireUser(auth.RequireRole(auth.RoleAdmin)(f))
	}
	mux.Handle("GET /admin/users", admin(h.users))
	mux.Handle("POST /admin/invitations", admin(h.invite))
	mux.Handle("GET /admin/users/{userID}", admin(h.user))
	mux.Handle("POST /admin/users/{userID}/role", admin(h.setRole))
	mux.Handle("POST /admin/users/{userID}/disable", admin(h.disable))
	mux.Handle("POST /admin/users/{userID}/enable", admin(h.enable))
	mux.Handle("POST /admin/users/{userID}/verification", admin(h.resendVerification))
	mux.Handle("POST /admin/users/{userID}/delete", admin(h.delete))
	mux.Handle("GET /admin/jobs", admin(h.jobs))
	mux.Handle("POST /admin/jobs/{jobID}/retry", admin(h.retryJob))
}

type usersPage struct {
	Search     string
	Users      []User
	Prev, Next string   // links to the neighbouring pages, if there are any
	Invite     web.Form // the invitation form
}

func (h *Web) users(w http.ResponseWriter, r *http.Request) {
	h.renderUsers(w, r, http.StatusOK, web.Form{})
}

// renderUsers shows the users matching the request's search, with invite
// as the invitation form.
func (h *Web) renderUsers(w http.ResponseWriter, r *http.Request, status int, invite web.Form) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	page = max(page, 1)
	users, more, err := h.svc.Users(r.Context(), q.Get("q"), page)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data := usersPage{Search: q.Get("q"), Users: users, Invite: invite}
	link := func(page int) string {
		q.Set("page", strconv.Itoa(page))
		return "/admin/users?" + q.Encode()
	}
	if page > 1 {
		data.Prev = link(page - 1)
	}
	if more {
		data.Next = link(page + 1)
	}
	h.pages.Render(w, r, status, "users.html", data)
}

func (h *Web) invite(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	form := web.Form{Values: r.PostForm}
	err := h.svc.Invite(r.Context(), form.Get("email"))
	switch {
	case errors.As(err, &form.Errors):
		h.renderUsers(w, r, http.StatusUnprocessableEntity, form)
	case err != nil:
		h.serverError(w, r, err)
	default:
		web.SetFlash(w, "An invitation is on its way to "+strings.TrimSpace(form.Get("email"))+".")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}
}

type userPage struct {
	User  User
	Self  bool   // the admin is looking at their own account
	Error string // why the last change was refused
}

func (h *Web) user(w http.ResponseWriter, r *http.Request) {
	h.renderUser(w, r, http.StatusOK, "")
}

func (h *Web) renderUser(w http.ResponseWriter, r *http.Request, status int, message string) {
	id, ok := httpx.PathID(r, "userID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	u, err := h.svc.User(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound)
		return
	} else if err != nil {
		h.serverError(w, r, err)
		return
	}
	me, _ := auth.UserFrom(r.Context())
	h.pages.Render(w, r, status, "user.html", userPage{User: u, Self: u.ID == me.ID, Error: message})
}

// change runs action on the user in the URL as the logged-in admin, then
// redirects to next (the user's page if empty) with message, or shows the
// user's page with why the action was refused.
func (h *Web) change(w http.ResponseWriter, r *http.Request, message, next string, action func(me auth.User, id uuid.UUID) error) {
	id, ok := httpx.PathID(r, "userID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	me, _ := auth.UserFrom(r.Context())
	err := action(me, id)
	var errs validate.Errors
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound)
	case errors.Is(err, ErrOwnAccount):
		h.renderUser(w, r, http.StatusForbidden, err.Error())
	case errors.As(err, &errs):
		h.renderUser(w, r, http.StatusUnprocessableEntity, strings.Join(slices.Sorted(maps.Values(errs)), " "))
	case err != nil:
		h.serverError(w, r, err)
	default:
		web.SetFlash(w, message)
		http.Redirect(w, r, cmp.Or(next, "/admin/users/"+id.String()), http.StatusSeeOther) //nolint:gosec // only a parsed UUID comes from the request
	}
}

func (h *Web) setRole(w http.ResponseWriter, r *http.Request) {
	role := web.PostForm(r).Get("role")
	h.change(w, r, "The role is changed.", "", func(me auth.User, id uuid.UUID) error {
		return h.svc.SetRole(r.Context(), me, id, role)
	})
}

func (h *Web) disable(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, "The account is disabled and logged out everywhere.", "", func(me auth.User, id uuid.UUID) error {
		return h.svc.SetDisabled(r.Context(), me, id, true)
	})
}

func (h *Web) enable(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, "The account is enabled.", "", func(me auth.User, id uuid.UUID) error {
		return h.svc.SetDisabled(r.Context(), me, id, false)
	})
}

func (h *Web) resendVerification(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, "A new verification link is on its way.", "", func(_ auth.User, id uuid.UUID) error {
		return h.svc.ResendVerification(r.Context(), id)
	})
}

func (h *Web) delete(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, "The account is deleted.", "/admin/users", func(me auth.User, id uuid.UUID) error {
		return h.svc.Delete(r.Context(), me, id)
	})
}

func (h *Web) jobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.svc.FailedJobs(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.pages.Render(w, r, http.StatusOK, "jobs.html", jobs)
}

func (h *Web) retryJob(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathID(r, "jobID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	err := h.svc.RetryJob(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound)
		return
	} else if err != nil {
		h.serverError(w, r, err)
		return
	}
	web.SetFlash(w, "The job will run again shortly.")
	http.Redirect(w, r, "/admin/jobs", http.StatusSeeOther)
}

func (h *Web) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.svc.logger.ErrorContext(r.Context(), "admin page", "err", err)
	httpx.Error(w, r, http.StatusInternalServerError)
}
