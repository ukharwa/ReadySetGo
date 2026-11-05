package admin

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// API serves the admin endpoints under /api/admin/, for admins only.
type API struct {
	svc *Service
}

// NewAPI returns the admin endpoints' handlers.
func NewAPI(svc *Service) *API {
	return &API{svc: svc}
}

// Routes registers the admin endpoints on mux. Lists are paged with ?page=
// (counting from 1); a page shorter than PageSize is the last.
func (h *API) Routes(mux *http.ServeMux) {
	admin := func(f http.HandlerFunc) http.Handler {
		return auth.RequireUser(auth.RequireRole(auth.RoleAdmin)(f))
	}
	mux.Handle("GET /api/admin/users", admin(h.users))
	mux.Handle("POST /api/admin/invitations", admin(h.invite))
	mux.Handle("GET /api/admin/users/{userID}", admin(h.user))
	mux.Handle("PATCH /api/admin/users/{userID}", admin(h.update))
	mux.Handle("DELETE /api/admin/users/{userID}", admin(h.delete))
	mux.Handle("POST /api/admin/users/{userID}/verification-email", admin(h.resendVerification))
	mux.Handle("GET /api/admin/failed-jobs", admin(h.jobs))
	mux.Handle("POST /api/admin/failed-jobs/{jobID}/retry", admin(h.retryJob))
}

type userJSON struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Role          string    `json:"role"`
	EmailVerified bool      `json:"email_verified"`
	Disabled      bool      `json:"disabled"`
	CreatedAt     time.Time `json:"created_at"`
}

type jobJSON struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Attempts  int64     `json:"attempts"`
	LastError string    `json:"last_error"`
	FailedAt  time.Time `json:"failed_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var errs validate.Errors
	switch {
	case errors.As(err, &errs):
		httpx.WriteInvalid(w, r, errs)
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound)
	case errors.Is(err, ErrOwnAccount):
		httpx.ErrorMessage(w, r, http.StatusForbidden, err.Error())
	default:
		h.svc.logger.ErrorContext(r.Context(), "admin endpoint", "path", r.URL.Path, "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
	}
}

func (h *API) users(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	users, _, err := h.svc.Users(r.Context(), r.URL.Query().Get("q"), page)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]userJSON, len(users))
	for i, u := range users {
		out[i] = userJSON(u)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *API) user(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathID(r, "userID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	u, err := h.svc.User(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, userJSON(u))
}

// update changes the fields given: {"role": "admin"}, {"disabled": true},
// or both. It responds with the user as they now are.
func (h *API) update(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathID(r, "userID")
	var in struct {
		Role     *string
		Disabled *bool
	}
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	me, _ := auth.UserFrom(r.Context())
	if in.Role != nil {
		if err := h.svc.SetRole(r.Context(), me, id, *in.Role); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	if in.Disabled != nil {
		if err := h.svc.SetDisabled(r.Context(), me, id, *in.Disabled); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	h.user(w, r)
}

func (h *API) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathID(r, "userID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	me, _ := auth.UserFrom(r.Context())
	if err := h.svc.Delete(r.Context(), me, id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// invite emails {"email": …} a link to sign up.
func (h *API) invite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	if err := h.svc.Invite(r.Context(), in.Email); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]string{"message": "An invitation is on its way."})
}

func (h *API) resendVerification(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathID(r, "userID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	if err := h.svc.ResendVerification(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]string{"message": "A new verification link is on its way, unless the address is already confirmed."})
}

func (h *API) jobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.svc.FailedJobs(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]jobJSON, len(jobs))
	for i, j := range jobs {
		out[i] = jobJSON(j)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *API) retryJob(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathID(r, "jobID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	if err := h.svc.RetryJob(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
