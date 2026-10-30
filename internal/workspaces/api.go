package workspaces

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/kickstart/internal/auth"
	"github.com/ukharwa/kickstart/internal/httpx"
	"github.com/ukharwa/kickstart/internal/validate"
)

// API serves workspaces as JSON under /api/workspaces/. Routes for one
// workspace (/api/workspaces/{workspaceID}/…) are the pattern for a feature
// whose data belongs to a workspace.
type API struct {
	svc *Service
}

// NewAPI returns the workspace endpoints' handlers.
func NewAPI(svc *Service) *API {
	return &API{svc: svc}
}

// Routes registers the workspace endpoints on mux.
func (h *API) Routes(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return auth.RequireUser(f) }
	member := func(f http.HandlerFunc) http.Handler { return auth.RequireUser(h.svc.Require(f)) }
	mux.Handle("GET /api/workspaces", user(h.list))
	mux.Handle("POST /api/workspaces", user(h.create))
	mux.Handle("GET /api/workspaces/{workspaceID}", member(h.get))
	mux.Handle("PATCH /api/workspaces/{workspaceID}", member(h.rename))
	mux.Handle("DELETE /api/workspaces/{workspaceID}", member(h.delete))
	mux.Handle("GET /api/workspaces/{workspaceID}/members", member(h.members))
	mux.Handle("PATCH /api/workspaces/{workspaceID}/members/{userID}", member(h.setRole))
	mux.Handle("DELETE /api/workspaces/{workspaceID}/members/{userID}", member(h.remove))
	mux.Handle("GET /api/workspaces/{workspaceID}/invitations", member(h.invitations))
	mux.Handle("POST /api/workspaces/{workspaceID}/invitations", member(h.invite))
	mux.Handle("DELETE /api/workspaces/{workspaceID}/invitations/{invitationID}", member(h.revoke))
	// The token travels in the body, not the URL, so it stays out of logs.
	mux.Handle("POST /api/invitations/lookup", user(h.lookup))
	mux.Handle("POST /api/invitations/accept", user(h.accept))
}

type workspaceJSON struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func toJSON(w Workspace) workspaceJSON {
	return workspaceJSON{ID: w.ID, Name: w.Name, Role: w.Role, CreatedAt: w.CreatedAt}
}

func (h *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var errs validate.Errors
	var wrong *WrongAddressError
	switch {
	case errors.As(err, &errs):
		httpx.WriteInvalid(w, r, errs)
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound)
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrLastOwner):
		httpx.ErrorMessage(w, r, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrInvalidInvitation):
		httpx.ErrorMessage(w, r, http.StatusBadRequest, "This invitation is invalid or has expired.")
	case errors.As(err, &wrong):
		httpx.ErrorMessage(w, r, http.StatusForbidden, "This invitation was sent to "+wrong.InvitedEmail+".")
	default:
		h.svc.logger.ErrorContext(r.Context(), "workspace endpoint", "path", r.URL.Path, "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
	}
}

func (h *API) list(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	all, err := h.svc.ForUser(r.Context(), u)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]workspaceJSON, len(all))
	for i, ws := range all {
		out[i] = toJSON(ws)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *API) create(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	u, _ := auth.UserFrom(r.Context())
	ws, err := h.svc.Create(r.Context(), u, in.Name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toJSON(ws))
}

func (h *API) get(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, toJSON(From(r.Context())))
}

func (h *API) rename(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	if err := h.svc.Rename(r.Context(), From(r.Context()), in.Name); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *API) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), From(r.Context())); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type memberJSON struct {
	UserID   uuid.UUID `json:"user_id"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

func (h *API) members(w http.ResponseWriter, r *http.Request) {
	all, err := h.svc.Members(r.Context(), From(r.Context()))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]memberJSON, len(all))
	for i, m := range all {
		out[i] = memberJSON(m)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *API) setRole(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "userID")
	var in struct{ Role string }
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	if err := h.svc.SetRole(r.Context(), From(r.Context()), id, in.Role); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *API) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "userID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	u, _ := auth.UserFrom(r.Context())
	if err := h.svc.RemoveMember(r.Context(), From(r.Context()), u, id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type invitationJSON struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (h *API) invitations(w http.ResponseWriter, r *http.Request) {
	all, err := h.svc.Invitations(r.Context(), From(r.Context()))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]invitationJSON, len(all))
	for i, inv := range all {
		out[i] = invitationJSON(inv)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *API) invite(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Role string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	u, _ := auth.UserFrom(r.Context())
	if err := h.svc.Invite(r.Context(), From(r.Context()), u, in.Email, in.Role); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]string{"message": "Invitation sent."})
}

func (h *API) revoke(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "invitationID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	if err := h.svc.RevokeInvitation(r.Context(), From(r.Context()), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *API) lookup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	inv, err := h.svc.LookupInvitation(r.Context(), in.Token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"workspace_name": inv.WorkspaceName, "email": inv.Email, "role": inv.Role,
	})
}

func (h *API) accept(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	u, _ := auth.UserFrom(r.Context())
	ws, err := h.svc.Accept(r.Context(), u, in.Token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toJSON(ws))
}
