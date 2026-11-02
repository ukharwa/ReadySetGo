package audit

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
)

// API serves the audit log as JSON to admins.
type API struct {
	svc *Service
}

// NewAPI returns the audit log endpoint's handler.
func NewAPI(svc *Service) *API {
	return &API{svc: svc}
}

// Routes registers the audit log endpoint on mux:
// GET /api/admin/audit-events?email=…&page=… lists PageSize events a page,
// newest first; a page shorter than that is the last.
func (h *API) Routes(mux *http.ServeMux) {
	mux.Handle("GET /api/admin/audit-events", auth.RequireUser(auth.RequireRole(auth.RoleAdmin)(http.HandlerFunc(h.list))))
}

type entryJSON struct {
	ID         uuid.UUID     `json:"id"`
	Event      string        `json:"event"`
	UserID     uuid.NullUUID `json:"user_id"`
	UserEmail  string        `json:"user_email,omitempty"`
	ActorID    uuid.NullUUID `json:"actor_id"`
	ActorEmail string        `json:"actor_email,omitempty"`
	Detail     string        `json:"detail,omitempty"`
	IP         string        `json:"ip,omitempty"`
	RequestID  string        `json:"request_id,omitempty"`
	At         time.Time     `json:"at"`
}

func (h *API) list(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	entries, _, err := h.svc.List(r.Context(), r.URL.Query().Get("email"), page)
	if err != nil {
		h.svc.logger.ErrorContext(r.Context(), "audit log endpoint", "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
		return
	}
	out := make([]entryJSON, len(entries))
	for i, e := range entries {
		out[i] = entryJSON(e)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
