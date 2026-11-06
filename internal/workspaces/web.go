package workspaces

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/web"
)

//go:embed templates
var templateFS embed.FS

// Web serves the workspace pages: the list of your workspaces, each
// workspace's settings (members and invitations), and accepting an
// invitation. The settings page is also the pattern for a feature whose data
// belongs to a workspace.
type Web struct {
	svc   *Service
	pages *web.Pages
}

// NewWeb returns the workspace pages' handlers.
func NewWeb(svc *Service, rd *web.Renderer) (*Web, error) {
	files, _ := fs.Sub(templateFS, "templates")
	pages, err := rd.Pages(files, "internal/workspaces/templates")
	if err != nil {
		return nil, err
	}
	return &Web{svc: svc, pages: pages}, nil
}

// Routes registers the workspace pages on mux.
func (h *Web) Routes(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return auth.RequireUser(f) }
	member := func(f http.HandlerFunc) http.Handler { return auth.RequireUser(h.svc.Require(f)) }
	mux.Handle("GET /workspaces", user(h.list))
	mux.Handle("POST /workspaces", user(h.create))
	mux.Handle("GET /w/{workspaceID}", member(h.show))
	mux.Handle("POST /w/{workspaceID}/name", member(h.rename))
	mux.Handle("POST /w/{workspaceID}/invitations", member(h.invite))
	mux.Handle("POST /w/{workspaceID}/invitations/{invitationID}/revoke", member(h.revoke))
	mux.Handle("POST /w/{workspaceID}/members/{userID}/role", member(h.setRole))
	mux.Handle("POST /w/{workspaceID}/members/{userID}/remove", member(h.remove))
	mux.Handle("POST /w/{workspaceID}/delete", member(h.delete))
	mux.Handle("GET /invitations/accept", user(h.acceptPage))
	mux.Handle("POST /invitations/accept", user(h.accept))
}

type listPage struct {
	Workspaces []Workspace
	Create     web.Form
}

func (h *Web) list(w http.ResponseWriter, r *http.Request) {
	h.renderList(w, r, http.StatusOK, web.Form{})
}

func (h *Web) renderList(w http.ResponseWriter, r *http.Request, status int, create web.Form) {
	u, _ := auth.UserFrom(r.Context())
	all, err := h.svc.ForUser(r.Context(), u)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.pages.Render(w, r, status, "workspaces.html", listPage{Workspaces: all, Create: create})
}

func (h *Web) create(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	form := web.Form{Values: web.PostForm(r)}
	ws, err := h.svc.Create(r.Context(), u, form.Get("name"))
	if errors.As(err, &form.Errors) {
		h.renderList(w, r, http.StatusUnprocessableEntity, form)
		return
	} else if err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/w/"+ws.ID.String(), http.StatusSeeOther)
}

// settingsPage is the data for a workspace's page, which holds several
// forms.
type settingsPage struct {
	Workspace   Workspace
	Me          uuid.UUID
	Members     []Member
	Invitations []Invitation
	Rename      web.Form
	Invite      web.Form
	Error       string // about the page as a whole
}

func (h *Web) show(w http.ResponseWriter, r *http.Request) {
	h.renderSettings(w, r, http.StatusOK, settingsPage{})
}

func (h *Web) renderSettings(w http.ResponseWriter, r *http.Request, status int, page settingsPage) {
	ctx := r.Context()
	u, _ := auth.UserFrom(ctx)
	page.Workspace, page.Me = From(ctx), u.ID
	var err error
	if page.Members, err = h.svc.Members(ctx, page.Workspace); err != nil {
		h.serverError(w, r, err)
		return
	}
	if page.Workspace.CanManage() {
		if page.Invitations, err = h.svc.Invitations(ctx, page.Workspace); err != nil {
			h.serverError(w, r, err)
			return
		}
	}
	h.pages.Render(w, r, status, "workspace.html", page)
}

// done redirects back to the workspace's page with a message.
func done(w http.ResponseWriter, r *http.Request, message string) {
	web.SetFlash(w, message)
	http.Redirect(w, r, "/w/"+From(r.Context()).ID.String(), http.StatusSeeOther)
}

// fail shows err on the workspace's page, on form when it is about one
// form's fields.
func (h *Web) fail(w http.ResponseWriter, r *http.Request, page settingsPage, form *web.Form, err error) {
	switch {
	case form != nil && errors.As(err, &form.Errors):
		h.renderSettings(w, r, http.StatusUnprocessableEntity, page)
	case errors.Is(err, ErrLastOwner), errors.Is(err, ErrForbidden):
		page.Error = httpx.Sentence(err)
		h.renderSettings(w, r, http.StatusForbidden, page)
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound)
	default:
		h.serverError(w, r, err)
	}
}

func (h *Web) rename(w http.ResponseWriter, r *http.Request) {
	page := settingsPage{Rename: web.Form{Values: web.PostForm(r)}}
	if err := h.svc.Rename(r.Context(), From(r.Context()), page.Rename.Get("name")); err != nil {
		h.fail(w, r, page, &page.Rename, err)
		return
	}
	done(w, r, "The workspace is renamed.")
}

func (h *Web) invite(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	page := settingsPage{Invite: web.Form{Values: web.PostForm(r)}}
	if err := h.svc.Invite(r.Context(), From(r.Context()), u, page.Invite.Get("email"), page.Invite.Get("role")); err != nil {
		h.fail(w, r, page, &page.Invite, err)
		return
	}
	done(w, r, "Invitation sent to "+page.Invite.Get("email")+".")
}

func (h *Web) revoke(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathID(r, "invitationID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	if err := h.svc.RevokeInvitation(r.Context(), From(r.Context()), id); err != nil {
		h.fail(w, r, settingsPage{}, nil, err)
		return
	}
	done(w, r, "The invitation is cancelled.")
}

func (h *Web) setRole(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathID(r, "userID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	if err := h.svc.SetRole(r.Context(), From(r.Context()), id, web.PostForm(r).Get("role")); err != nil {
		h.fail(w, r, settingsPage{}, nil, err)
		return
	}
	done(w, r, "The role is changed.")
}

func (h *Web) remove(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	id, ok := httpx.PathID(r, "userID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	if err := h.svc.RemoveMember(r.Context(), From(r.Context()), u, id); err != nil {
		h.fail(w, r, settingsPage{}, nil, err)
		return
	}
	if id == u.ID {
		web.SetFlash(w, "You've left “"+From(r.Context()).Name+"”.")
		http.Redirect(w, r, "/workspaces", http.StatusSeeOther)
		return
	}
	done(w, r, "The member is removed.")
}

func (h *Web) delete(w http.ResponseWriter, r *http.Request) {
	ws := From(r.Context())
	if err := h.svc.Delete(r.Context(), ws); err != nil {
		h.fail(w, r, settingsPage{}, nil, err)
		return
	}
	web.SetFlash(w, "“"+ws.Name+"” is deleted.")
	http.Redirect(w, r, "/workspaces", http.StatusSeeOther)
}

type acceptPage struct {
	Token      string
	Invitation PendingInvitation
	Error      string
}

func (h *Web) acceptPage(w http.ResponseWriter, r *http.Request) {
	page := acceptPage{Token: r.URL.Query().Get("token")}
	inv, err := h.svc.LookupInvitation(r.Context(), page.Token)
	switch {
	case errors.Is(err, ErrInvalidInvitation):
		page.Error = "This invitation is invalid or has expired. Ask for a new one."
	case err != nil:
		h.serverError(w, r, err)
		return
	}
	page.Invitation = inv
	h.pages.Render(w, r, http.StatusOK, "accept_invitation.html", page)
}

func (h *Web) accept(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	token := web.PostForm(r).Get("token")
	ws, err := h.svc.Accept(r.Context(), u, token)
	var wrong *WrongAddressError
	switch {
	case errors.Is(err, ErrInvalidInvitation):
		h.pages.Render(w, r, http.StatusBadRequest, "accept_invitation.html",
			acceptPage{Error: "This invitation is invalid or has expired. Ask for a new one."})
		return
	case errors.As(err, &wrong):
		h.pages.Render(w, r, http.StatusForbidden, "accept_invitation.html", acceptPage{Error: "This invitation was sent to " +
			wrong.InvitedEmail + ". Log out, then log in or sign up with that address to accept it."})
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}
	web.SetFlash(w, "You've joined “"+ws.Name+"”.")
	http.Redirect(w, r, "/w/"+ws.ID.String(), http.StatusSeeOther)
}

func (h *Web) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.svc.logger.ErrorContext(r.Context(), "workspace page", "err", err)
	httpx.Error(w, r, http.StatusInternalServerError)
}
