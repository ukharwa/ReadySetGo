package audit

import (
	"embed"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/ukharwa/kickstart/internal/auth"
	"github.com/ukharwa/kickstart/internal/httpx"
	"github.com/ukharwa/kickstart/internal/web"
)

//go:embed templates
var templateFS embed.FS

// Web serves the audit log page to admins.
type Web struct {
	svc   *Service
	pages *web.Pages
}

// NewWeb returns the audit log page's handler.
func NewWeb(svc *Service, rd *web.Renderer) (*Web, error) {
	files, _ := fs.Sub(templateFS, "templates")
	pages, err := rd.Pages(files, "internal/audit/templates")
	if err != nil {
		return nil, err
	}
	return &Web{svc: svc, pages: pages}, nil
}

// Routes registers the audit log page on mux.
func (h *Web) Routes(mux *http.ServeMux) {
	mux.Handle("GET /admin/audit", auth.RequireUser(auth.RequireRole(auth.RoleAdmin)(http.HandlerFunc(h.list))))
}

type listPage struct {
	Email        string
	Entries      []Entry
	Newer, Older string // links to the neighbouring pages, if there are any
}

func (h *Web) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	page = max(page, 1)
	entries, more, err := h.svc.List(r.Context(), q.Get("email"), page)
	if err != nil {
		h.svc.logger.ErrorContext(r.Context(), "audit log page", "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
		return
	}
	data := listPage{Email: q.Get("email"), Entries: entries}
	link := func(page int) string {
		q.Set("page", strconv.Itoa(page))
		return "/admin/audit?" + q.Encode()
	}
	if page > 1 {
		data.Newer = link(page - 1)
	}
	if more {
		data.Older = link(page + 1)
	}
	h.pages.Render(w, r, http.StatusOK, "audit.html", data)
}
