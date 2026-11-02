package uploads

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"github.com/ukharwa/kickstart/internal/auth"
	"github.com/ukharwa/kickstart/internal/httpx"
	"github.com/ukharwa/kickstart/internal/validate"
	"github.com/ukharwa/kickstart/internal/web"
)

//go:embed templates
var templateFS embed.FS

// Web serves the files page, where users upload, download and delete their
// files.
type Web struct {
	svc   *Service
	pages *web.Pages
}

// NewWeb returns the files page's handlers.
func NewWeb(svc *Service, rd *web.Renderer) (*Web, error) {
	files, _ := fs.Sub(templateFS, "templates")
	pages, err := rd.Pages(files, "internal/uploads/templates")
	if err != nil {
		return nil, err
	}
	return &Web{svc: svc, pages: pages}, nil
}

// Routes registers the files page on mux.
func (h *Web) Routes(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return auth.RequireUser(f) }
	mux.Handle("GET /uploads", user(h.list))
	mux.Handle("POST /uploads", auth.RequireUser(limitUpload(http.HandlerFunc(h.upload))))
	mux.Handle("GET /uploads/{uploadID}", user(h.download))
	mux.Handle("POST /uploads/{uploadID}/delete", user(h.delete))
}

type listPage struct {
	Uploads []Upload
	Accept  string // the allowed types, for the file input
	MaxMB   int
	Error   string
}

func (h *Web) list(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "")
}

func (h *Web) render(w http.ResponseWriter, r *http.Request, status int, message string) {
	u, _ := auth.UserFrom(r.Context())
	all, err := h.svc.ForUser(r.Context(), u.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.pages.Render(w, r, status, "uploads.html", listPage{
		Uploads: all, Accept: strings.Join(allowedTypes, ","), MaxMB: MaxSize >> 20, Error: message,
	})
}

func (h *Web) upload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := uploadContext(w, r)
	defer cancel()
	r = r.WithContext(ctx)
	u, _ := auth.UserFrom(ctx)
	var saved Upload
	part, err := filePart(r)
	if err == nil {
		saved, err = h.svc.Save(ctx, u.ID, part.FileName(), part)
	}
	var errs validate.Errors
	if errors.As(err, &errs) {
		h.render(w, r, http.StatusUnprocessableEntity, errs["file"])
		return
	} else if err != nil {
		h.serverError(w, r, err)
		return
	}
	web.SetFlash(w, "Uploaded “"+saved.Filename+"”.")
	http.Redirect(w, r, "/uploads", http.StatusSeeOther)
}

func (h *Web) download(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "uploadID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	me, _ := auth.UserFrom(r.Context())
	u, err := h.svc.Get(r.Context(), me.ID, id)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound)
		return
	} else if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.svc.serveFile(w, r, u)
}

func (h *Web) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "uploadID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	me, _ := auth.UserFrom(r.Context())
	err := h.svc.Delete(r.Context(), me.ID, id)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound)
		return
	} else if err != nil {
		h.serverError(w, r, err)
		return
	}
	web.SetFlash(w, "The file is deleted.")
	http.Redirect(w, r, "/uploads", http.StatusSeeOther)
}

func (h *Web) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.svc.logger.ErrorContext(r.Context(), "uploads page", "err", err)
	httpx.Error(w, r, http.StatusInternalServerError)
}
