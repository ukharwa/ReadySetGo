package uploads_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ukharwa/readysetgo/internal/uploads"
)

func TestWebUploads(t *testing.T) {
	t.Parallel()
	a, svc, dir := newApp(t)
	pages, err := uploads.NewWeb(svc, a.Renderer())
	if err != nil {
		t.Fatal(err)
	}
	pages.Routes(a.Mux)
	ada, eve := a.Client(), a.Client()
	a.SignIn(ada, "ada@example.com")
	a.SignIn(eve, "eve@example.com")

	if r := postFile(t, a, ada, "/uploads", "page.html", html); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Body, "Upload an image") {
		t.Errorf("HTML upload: %d", r.Status)
	}
	if r := postFile(t, a, ada, "/uploads", "report.pdf", pdf); r.Status != http.StatusSeeOther {
		t.Fatalf("upload: %d\n%s", r.Status, r.Body)
	}
	list, err := svc.ForUser(t.Context(), a.SignIn(a.Client(), "x@example.com").ID)
	if err != nil || len(list) != 0 {
		t.Errorf("someone else's list: %v %v", list, err)
	}
	page := get(t, a, ada, "/uploads")
	if !strings.Contains(page.Body, "report.pdf") {
		t.Fatalf("list:\n%s", page.Body)
	}
	link := page.Body[strings.Index(page.Body, `href="/uploads/`)+len(`href="`):]
	link = link[:strings.Index(link, `"`)]

	r := get(t, a, ada, link)
	if r.Status != http.StatusOK || r.Body != string(pdf) || r.Header.Get("Content-Type") != "application/pdf" ||
		r.Header.Get("Content-Disposition") != `attachment; filename=report.pdf` ||
		!strings.Contains(r.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("download: %d %v", r.Status, r.Header)
	}
	if r := get(t, a, eve, link); r.Status != http.StatusNotFound {
		t.Errorf("someone else downloading: %d, want 404", r.Status)
	}

	if r := postFile(t, a, eve, link+"/delete", "", nil); r.Status != http.StatusNotFound {
		t.Errorf("someone else deleting: %d, want 404", r.Status)
	}
	if r := postFile(t, a, ada, link+"/delete", "", nil); r.Status != http.StatusSeeOther {
		t.Fatalf("delete: %d", r.Status)
	}
	runJobs(t, a)
	if n := stored(t, dir); n != 0 {
		t.Errorf("%d files left after deleting", n)
	}
}

func TestWebUploadTooBig(t *testing.T) {
	t.Parallel()
	a, svc, _ := newApp(t)
	pages, err := uploads.NewWeb(svc, a.Renderer())
	if err != nil {
		t.Fatal(err)
	}
	pages.Routes(a.Mux)
	c := a.Client()
	a.SignIn(c, "ada@example.com")
	big := append(append([]byte{}, png...), make([]byte, uploads.MaxSize+100<<10)...)
	if r := postFile(t, a, c, "/uploads", "big.png", big); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Body, "up to 10 MB") {
		t.Errorf("too big: %d", r.Status)
	}
}
