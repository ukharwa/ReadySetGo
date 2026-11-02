package uploads_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ukharwa/kickstart/internal/uploads"
)

func TestAPIUploads(t *testing.T) {
	t.Parallel()
	a, svc, _ := newApp(t)
	uploads.NewAPI(svc).Routes(a.Mux)
	ada, eve := a.Client(), a.Client()
	a.SignIn(ada, "ada@example.com")
	a.SignIn(eve, "eve@example.com")

	if r := postFile(t, a, ada, "/api/uploads", "page.html", html); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("HTML upload: %d %s", r.Status, r.Body)
	}
	r := postFile(t, a, ada, "/api/uploads", "photo.png", png)
	var created struct {
		ID, Filename, URL string
		ContentType       string `json:"content_type"`
	}
	if err := json.Unmarshal([]byte(r.Body), &created); err != nil {
		t.Fatalf("upload: %d %s", r.Status, r.Body)
	}
	if r.Status != http.StatusCreated || created.ContentType != "image/png" || created.URL == "" {
		t.Fatalf("upload: %d %s", r.Status, r.Body)
	}

	var list []struct{ ID string }
	a.Decode(a.JSON(ada, "GET", "/api/uploads", "", nil), &list)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("list: %+v", list)
	}
	if r := get(t, a, ada, created.URL); r.Status != http.StatusOK || r.Body != string(png) ||
		r.Header.Get("Content-Disposition") != "inline; filename=photo.png" {
		t.Errorf("download: %d %v", r.Status, r.Header)
	}
	if r := get(t, a, eve, created.URL); r.Status != http.StatusNotFound {
		t.Errorf("someone else downloading: %d, want 404", r.Status)
	}
	if r := a.JSON(eve, "DELETE", created.URL, "", nil); r.Status != http.StatusNotFound {
		t.Errorf("someone else deleting: %d, want 404", r.Status)
	}
	if r := a.JSON(ada, "DELETE", created.URL, "", nil); r.Status != http.StatusNoContent {
		t.Errorf("delete: %d", r.Status)
	}
}
