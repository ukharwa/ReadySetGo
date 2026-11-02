package notes_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/apptest"
	"github.com/ukharwa/readysetgo/internal/notes"
)

func newWebApp(t *testing.T) *apptest.App {
	t.Helper()
	a := apptest.New(t)
	pages, err := notes.NewWeb(notes.NewService(a.DB, a.Logger), a.Renderer())
	if err != nil {
		t.Fatal(err)
	}
	pages.Routes(a.Mux)
	return a
}

func TestWebNotes(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	c := a.Client()
	a.SignIn(c, "ada@example.com")

	if p := a.Post(c, "/notes", url.Values{"title": {"  "}, "body": {"x"}}); p.Status != http.StatusUnprocessableEntity || !strings.Contains(p.Body, "Enter a title") {
		t.Errorf("empty title: %d", p.Status)
	}
	p := a.Post(c, "/notes", url.Values{"title": {"Shopping"}, "body": {"eggs\nmilk"}})
	if p.Status != http.StatusSeeOther || !strings.HasPrefix(p.Location, "/notes/") {
		t.Fatalf("create: %d → %q", p.Status, p.Location)
	}
	note := p.Location
	if p := a.Get(c, note); !apptest.Contains(p.Body, "Note saved.", "Shopping", "eggs\nmilk") {
		t.Errorf("note page:\n%s", p.Body)
	}
	if p := a.Get(c, note+"/edit"); !apptest.Contains(p.Body, `value="Shopping"`, `action="`+note+`"`) {
		t.Errorf("edit form:\n%s", p.Body)
	}
	if p := a.Post(c, note, url.Values{"title": {"Groceries"}, "body": {"eggs"}}); p.Status != http.StatusSeeOther {
		t.Fatalf("update: %d\n%s", p.Status, p.Body)
	}
	if p := a.Get(c, "/notes"); !strings.Contains(p.Body, "Groceries") || strings.Contains(p.Body, "Shopping") {
		t.Errorf("list after update:\n%s", p.Body)
	}
	if p := a.Post(c, note+"/delete", nil); p.Status != http.StatusSeeOther || p.Location != "/notes" {
		t.Errorf("delete: %d → %q", p.Status, p.Location)
	}
	if p := a.Get(c, note); p.Status != http.StatusNotFound {
		t.Errorf("deleted note: %d, want 404", p.Status)
	}
}

// TestWebDeleteWithHTMX checks that HTMX gets an empty response to swap
// in, not a redirect.
func TestWebDeleteWithHTMX(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	c := a.Client()
	a.SignIn(c, "ada@example.com")
	note := a.Post(c, "/notes", url.Values{"title": {"Temp"}}).Location

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, a.URL(note+"/delete"), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || res.ContentLength > 0 {
		t.Errorf("HTMX delete: %d, %d bytes; want 200 and nothing", res.StatusCode, res.ContentLength)
	}
}

// TestWebIsolation checks that one user can't see or change another's
// notes: every route answers 404, as if the note didn't exist.
func TestWebIsolation(t *testing.T) {
	t.Parallel()
	a := newWebApp(t)
	ada, eve := a.Client(), a.Client()
	a.SignIn(ada, "ada@example.com")
	a.SignIn(eve, "eve@example.com")
	note := a.Post(ada, "/notes", url.Values{"title": {"Private"}}).Location

	for _, path := range []string{note, note + "/edit"} {
		if p := a.Get(eve, path); p.Status != http.StatusNotFound {
			t.Errorf("GET %s as someone else: %d, want 404", path, p.Status)
		}
	}
	for _, path := range []string{note, note + "/delete"} {
		if p := a.Post(eve, path, url.Values{"title": {"Mine now"}}); p.Status != http.StatusNotFound {
			t.Errorf("POST %s as someone else: %d, want 404", path, p.Status)
		}
	}
	if p := a.Get(eve, "/notes"); strings.Contains(p.Body, "Private") {
		t.Error("someone else's note is in the list")
	}
	if p := a.Get(ada, note); !strings.Contains(p.Body, "Private") {
		t.Error("the note changed")
	}
}
