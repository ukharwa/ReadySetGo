package notes

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// API serves notes as JSON under /api/notes/.
type API struct {
	svc *Service
}

// NewAPI returns the notes endpoints' handlers.
func NewAPI(svc *Service) *API {
	return &API{svc: svc}
}

// Routes registers the notes endpoints on mux, for logged-in users.
func (h *API) Routes(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return auth.RequireUser(f) }
	mux.Handle("GET /api/notes", user(h.list))
	mux.Handle("POST /api/notes", user(h.create))
	mux.Handle("GET /api/notes/{noteID}", user(h.get))
	mux.Handle("PUT /api/notes/{noteID}", user(h.update))
	mux.Handle("DELETE /api/notes/{noteID}", user(h.delete))
}

type noteJSON struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// noteInput is what POST and PUT take.
type noteInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (h *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var errs validate.Errors
	switch {
	case errors.As(err, &errs):
		httpx.WriteInvalid(w, r, errs)
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound)
	default:
		h.svc.logger.ErrorContext(r.Context(), "notes endpoint", "path", r.URL.Path, "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
	}
}

func (h *API) list(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.UserFrom(r.Context())
	notes, err := h.svc.List(r.Context(), me.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]noteJSON, len(notes))
	for i, n := range notes {
		out[i] = noteJSON(n)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *API) create(w http.ResponseWriter, r *http.Request) {
	var in noteInput
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	me, _ := auth.UserFrom(r.Context())
	note, err := h.svc.Create(r.Context(), me.ID, in.Title, in.Body)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, noteJSON(note))
}

func (h *API) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "noteID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	me, _ := auth.UserFrom(r.Context())
	note, err := h.svc.Get(r.Context(), me.ID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, noteJSON(note))
}

func (h *API) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "noteID")
	var in noteInput
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	me, _ := auth.UserFrom(r.Context())
	note, err := h.svc.Update(r.Context(), me.ID, id, in.Title, in.Body)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, noteJSON(note))
}

func (h *API) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "noteID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	me, _ := auth.UserFrom(r.Context())
	if err := h.svc.Delete(r.Context(), me.ID, id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
