package uploads

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/kickstart/internal/auth"
	"github.com/ukharwa/kickstart/internal/httpx"
	"github.com/ukharwa/kickstart/internal/validate"
)

// API serves uploads under /api/uploads/: upload with a multipart form
// whose "file" field holds the file, list, download and delete.
type API struct {
	svc *Service
}

// NewAPI returns the upload endpoints' handlers.
func NewAPI(svc *Service) *API {
	return &API{svc: svc}
}

// Routes registers the upload endpoints on mux.
func (h *API) Routes(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return auth.RequireUser(f) }
	mux.Handle("GET /api/uploads", user(h.list))
	mux.Handle("POST /api/uploads", auth.RequireUser(limitUpload(http.HandlerFunc(h.upload))))
	mux.Handle("GET /api/uploads/{uploadID}", user(h.download))
	mux.Handle("DELETE /api/uploads/{uploadID}", user(h.delete))
}

type uploadJSON struct {
	ID          uuid.UUID `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
	URL         string    `json:"url"` // where to download it
}

func toJSON(u Upload) uploadJSON {
	return uploadJSON{
		ID: u.ID, Filename: u.Filename, ContentType: u.ContentType, Size: u.Size,
		CreatedAt: u.CreatedAt, URL: "/api/uploads/" + u.ID.String(),
	}
}

func (h *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var errs validate.Errors
	switch {
	case errors.As(err, &errs):
		httpx.WriteInvalid(w, r, errs)
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound)
	default:
		h.svc.logger.ErrorContext(r.Context(), "uploads endpoint", "path", r.URL.Path, "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
	}
}

func (h *API) list(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.UserFrom(r.Context())
	all, err := h.svc.ForUser(r.Context(), me.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]uploadJSON, len(all))
	for i, u := range all {
		out[i] = toJSON(u)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *API) upload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := uploadContext(w, r)
	defer cancel()
	r = r.WithContext(ctx)
	me, _ := auth.UserFrom(ctx)
	part, err := filePart(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	u, err := h.svc.Save(ctx, me.ID, part.FileName(), part)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toJSON(u))
}

func (h *API) download(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "uploadID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	me, _ := auth.UserFrom(r.Context())
	u, err := h.svc.Get(r.Context(), me.ID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.svc.serveFile(w, r, u)
}

func (h *API) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "uploadID")
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
