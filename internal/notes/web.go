package notes

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"net/url"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/web"
)

//go:embed templates
var templateFS embed.FS

// Web serves the notes pages.
type Web struct {
	svc   *Service
	pages *web.Pages
}

// NewWeb returns the notes pages' handlers.
func NewWeb(svc *Service, rd *web.Renderer) (*Web, error) {
	files, _ := fs.Sub(templateFS, "templates")
	pages, err := rd.Pages(files, "internal/notes/templates")
	if err != nil {
		return nil, err
	}
	return &Web{svc: svc, pages: pages}, nil
}

// Routes registers the notes pages on mux, for logged-in users.
func (h *Web) Routes(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return auth.RequireUser(f) }
	mux.Handle("GET /notes", user(h.list))
	mux.Handle("GET /notes/new", user(h.newForm))
	mux.Handle("POST /notes", user(h.create))
	mux.Handle("GET /notes/{noteID}", user(h.show))
	mux.Handle("GET /notes/{noteID}/edit", user(h.editForm))
	mux.Handle("POST /notes/{noteID}", user(h.update))
	mux.Handle("POST /notes/{noteID}/delete", user(h.delete))
}

func (h *Web) list(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.UserFrom(r.Context())
	notes, err := h.svc.List(r.Context(), me.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.pages.Render(w, r, http.StatusOK, "notes.html", notes)
}

// formPage is the data for the new and edit forms.
type formPage struct {
	Heading string
	Action  string // where the form posts
	Form    web.Form
}

func newPage(form web.Form) formPage {
	return formPage{Heading: "New note", Action: "/notes", Form: form}
}

func editPage(note Note, form web.Form) formPage {
	return formPage{Heading: "Edit note", Action: "/notes/" + note.ID.String(), Form: form}
}

func (h *Web) newForm(w http.ResponseWriter, r *http.Request) {
	h.pages.Render(w, r, http.StatusOK, "form.html", newPage(web.Form{}))
}

func (h *Web) create(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.UserFrom(r.Context())
	form := web.Form{Values: web.PostForm(r)}
	note, err := h.svc.Create(r.Context(), me.ID, form.Get("title"), form.Get("body"))
	if errors.As(err, &form.Errors) {
		h.pages.Render(w, r, http.StatusUnprocessableEntity, "form.html", newPage(form))
		return
	} else if err != nil {
		h.serverError(w, r, err)
		return
	}
	web.SetFlash(w, "Note saved.")
	http.Redirect(w, r, "/notes/"+note.ID.String(), http.StatusSeeOther)
}

// note loads the note in the URL, answering 404 itself if there is none.
func (h *Web) note(w http.ResponseWriter, r *http.Request) (Note, bool) {
	id, ok := httpx.PathID(r, "noteID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return Note{}, false
	}
	me, _ := auth.UserFrom(r.Context())
	note, err := h.svc.Get(r.Context(), me.ID, id)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound)
		return Note{}, false
	} else if err != nil {
		h.serverError(w, r, err)
		return Note{}, false
	}
	return note, true
}

func (h *Web) show(w http.ResponseWriter, r *http.Request) {
	if note, ok := h.note(w, r); ok {
		h.pages.Render(w, r, http.StatusOK, "note.html", note)
	}
}

func (h *Web) editForm(w http.ResponseWriter, r *http.Request) {
	note, ok := h.note(w, r)
	if !ok {
		return
	}
	values := url.Values{"title": {note.Title}, "body": {note.Body}}
	h.pages.Render(w, r, http.StatusOK, "form.html", editPage(note, web.Form{Values: values}))
}

func (h *Web) update(w http.ResponseWriter, r *http.Request) {
	note, ok := h.note(w, r)
	if !ok {
		return
	}
	me, _ := auth.UserFrom(r.Context())
	form := web.Form{Values: web.PostForm(r)}
	_, err := h.svc.Update(r.Context(), me.ID, note.ID, form.Get("title"), form.Get("body"))
	switch {
	case errors.As(err, &form.Errors):
		h.pages.Render(w, r, http.StatusUnprocessableEntity, "form.html", editPage(note, form))
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound) // deleted meanwhile
	case err != nil:
		h.serverError(w, r, err)
	default:
		web.SetFlash(w, "Note saved.")
		http.Redirect(w, r, "/notes/"+note.ID.String(), http.StatusSeeOther)
	}
}

// delete deletes a note. HTMX (the list's delete buttons) gets an empty
// response that replaces the note's row; a plain form post is redirected
// to the list.
func (h *Web) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathID(r, "noteID")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound)
		return
	}
	me, _ := auth.UserFrom(r.Context())
	err := h.svc.Delete(r.Context(), me.ID, id)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound)
	case err != nil:
		h.serverError(w, r, err)
	case web.IsHTMX(r):
		w.WriteHeader(http.StatusOK)
	default:
		web.SetFlash(w, "Note deleted.")
		http.Redirect(w, r, "/notes", http.StatusSeeOther)
	}
}

func (h *Web) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.svc.logger.ErrorContext(r.Context(), "notes page", "err", err)
	httpx.Error(w, r, http.StatusInternalServerError)
}
