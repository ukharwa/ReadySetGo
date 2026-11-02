package notes_test

import (
	"net/http"
	"testing"

	"github.com/ukharwa/kickstart/internal/apptest"
	"github.com/ukharwa/kickstart/internal/notes"
)

type noteJSON struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func TestAPINotes(t *testing.T) {
	t.Parallel()
	a := apptest.New(t)
	notes.NewAPI(notes.NewService(a.DB, a.Logger)).Routes(a.Mux)
	ada, eve := a.Client(), a.Client()
	a.SignIn(ada, "ada@example.com")
	a.SignIn(eve, "eve@example.com")

	if r := a.JSON(ada, "POST", "/api/notes", "", map[string]string{"title": ""}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("empty title: %d", r.Status)
	}
	var created noteJSON
	r := a.JSON(ada, "POST", "/api/notes", "", map[string]string{"title": "Shopping", "body": "eggs"})
	a.Decode(r, &created)
	if r.Status != http.StatusCreated || created.Title != "Shopping" {
		t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	path := "/api/notes/" + created.ID

	var updated noteJSON
	a.Decode(a.JSON(ada, "PUT", path, "", map[string]string{"title": "Groceries", "body": "milk"}), &updated)
	if updated.Title != "Groceries" || updated.Body != "milk" {
		t.Errorf("update: %+v", updated)
	}
	var list []noteJSON
	a.Decode(a.JSON(ada, "GET", "/api/notes", "", nil), &list)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("list: %+v", list)
	}

	// Someone else gets 404 for everything about the note.
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if r := a.JSON(eve, method, path, "", map[string]string{"title": "Mine now"}); r.Status != http.StatusNotFound {
			t.Errorf("%s as someone else: %d, want 404", method, r.Status)
		}
	}
	if r := a.JSON(ada, "DELETE", path, "", nil); r.Status != http.StatusNoContent {
		t.Errorf("delete: %d", r.Status)
	}
	if r := a.JSON(ada, "GET", path, "", nil); r.Status != http.StatusNotFound {
		t.Errorf("deleted note: %d, want 404", r.Status)
	}
}
