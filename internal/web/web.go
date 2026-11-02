// Package web renders HTML pages: the shared layout, each feature's page
// templates, static files (CSS and HTMX) with cache-busting URLs, and flash
// messages.
//
// Templates and static files are embedded in the binary. In dev, when run
// from the repository root (`make run`), they are read from disk on every
// request instead, so edits show without a restart.
package web

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ukharwa/kickstart/internal/validate"
)

//go:embed templates static
var embedded embed.FS

// devDir is this package's folder, relative to the repository root, where
// `make run` reads templates and static files from in dev.
const devDir = "internal/web"

// Options configures a Renderer.
type Options struct {
	AppName string
	Dev     bool
	// User returns the logged-in user for the layout, or nil.
	User   func(ctx context.Context) any
	Logger *slog.Logger
}

// Renderer renders pages within the shared layout and serves static files.
type Renderer struct {
	opts   Options
	files  fs.FS             // this package's templates/ and static/
	hashes map[string]string // static file name → content hash, in production
}

// New returns a Renderer, hashing the static files for their URLs.
func New(opts Options) (*Renderer, error) {
	rd := &Renderer{opts: opts, files: embedded, hashes: map[string]string{}}
	if opts.Dev && isDir(devDir) {
		rd.files = os.DirFS(devDir)
		return rd, nil
	}
	err := fs.WalkDir(embedded, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(embedded, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		rd.hashes[strings.TrimPrefix(p, "static/")] = hex.EncodeToString(sum[:6])
		return nil
	})
	return rd, err
}

// asset returns the URL of a static file. In production it carries the
// file's content hash, so browsers can cache it for a year and still get
// each new version.
func (rd *Renderer) asset(name string) string {
	if h, ok := rd.hashes[name]; ok {
		return "/static/" + name + "?v=" + h
	}
	return "/static/" + name
}

// Static serves the static files under /static/.
func (rd *Renderer) Static() http.Handler {
	files, _ := fs.Sub(rd.files, "static")
	server := http.StripPrefix("/static/", http.FileServerFS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/static/")
		if h, ok := rd.hashes[name]; ok && r.URL.Query().Get("v") == h {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		server.ServeHTTP(w, r)
	})
}

// Pages is a feature's set of page templates.
type Pages struct {
	rd     *Renderer
	files  fs.FS
	byName map[string]*template.Template
}

// Pages parses every *.html file in fsys, each within the layout. devDir is
// the same folder on disk, relative to the repository root, read on every
// render in dev. Parsing everything at startup means a broken template
// stops the app starting rather than failing one page later.
func (rd *Renderer) Pages(fsys fs.FS, devDir string) (*Pages, error) {
	p := &Pages{rd: rd, files: fsys}
	live := rd.opts.Dev && isDir(devDir)
	if live {
		p.files = os.DirFS(devDir)
	}
	byName, err := p.parse()
	if err != nil {
		return nil, err
	}
	if !live {
		p.byName = byName
	}
	return p, nil
}

// isDir reports whether path is a folder: in dev, whether the source tree is
// there to read from.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (p *Pages) parse() (map[string]*template.Template, error) {
	layout, err := template.New("layout").Funcs(template.FuncMap{"asset": p.rd.asset}).
		ParseFS(p.rd.files, "templates/layout.html")
	if err != nil {
		return nil, fmt.Errorf("parse layout: %w", err)
	}
	names, err := fs.Glob(p.files, "*.html")
	if err != nil {
		return nil, err
	}
	byName := map[string]*template.Template{}
	for _, name := range names {
		t, err := template.Must(layout.Clone()).ParseFS(p.files, name)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		byName[name] = t
	}
	return byName, nil
}

// pageData is what every template receives: .AppName, .User and .Flash from
// the layout's side, and .Data from the handler.
type pageData struct {
	AppName string
	User    any
	Flash   string
	Data    any
}

// Render writes the page name with status, passing data as .Data.
func (p *Pages) Render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	byName := p.byName
	if byName == nil { // dev: pick up edits
		var err error
		if byName, err = p.parse(); err != nil {
			p.fail(w, r, err)
			return
		}
	}
	t, ok := byName[name]
	if !ok {
		p.fail(w, r, fmt.Errorf("no page template %q", name))
		return
	}
	pd := pageData{AppName: p.rd.opts.AppName, Flash: popFlash(w, r), Data: data}
	if p.rd.opts.User != nil {
		pd.User = p.rd.opts.User(r.Context())
	}
	// Render to a buffer first, so a template error mid-page becomes a
	// clean error response rather than half a page.
	var b strings.Builder
	if err := t.ExecuteTemplate(&b, "layout", pd); err != nil {
		p.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, b.String())
}

func (p *Pages) fail(w http.ResponseWriter, r *http.Request, err error) {
	p.rd.opts.Logger.ErrorContext(r.Context(), "render page", "err", err)
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}

// Form carries a submitted form back to its template, with the values to
// refill and any errors per field.
type Form struct {
	Values url.Values
	Errors validate.Errors
}

// Get returns a submitted value.
func (f Form) Get(field string) string { return f.Values.Get(field) }

const flashCookie = "__Host-flash"

// SetFlash shows message on the next page this browser renders, typically
// after a redirect.
func SetFlash(w http.ResponseWriter, message string) {
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: url.QueryEscape(message), Path: "/", MaxAge: int(time.Minute / time.Second),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// popFlash returns the flash message, if any, and clears it.
func popFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	msg, _ := url.QueryUnescape(c.Value)
	return msg
}

// IsHTMX reports whether HTMX sent r, so a handler can answer with just the
// part of the page that changed instead of redirecting.
func IsHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// SafeRedirect returns next if it is a path on this site, and fallback
// otherwise, so a ?next= parameter can't send users to another site.
func SafeRedirect(next, fallback string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, `\`) {
		return fallback
	}
	if u, err := url.Parse(next); err != nil || u.Host != "" || u.Scheme != "" || path.Clean(u.Path) == "" {
		return fallback
	}
	return next
}

// Home serves the home page; register it for "GET /{$}". Replace its
// template with your app's own.
func (rd *Renderer) Home() (http.Handler, error) {
	own, _ := fs.Sub(embedded, "templates/pages")
	pages, err := rd.Pages(own, devDir+"/templates/pages")
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages.Render(w, r, http.StatusOK, "home.html", nil)
	}), nil
}
