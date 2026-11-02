package uploads_test

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ukharwa/kickstart/internal/apptest"
	"github.com/ukharwa/kickstart/internal/uploads"
	"github.com/ukharwa/kickstart/internal/validate"
)

// Files whose type http.DetectContentType recognises from the first bytes.
var (
	png  = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)
	pdf  = []byte("%PDF-1.7\n1 0 obj << >> endobj\n")
	html = []byte("<!DOCTYPE html><script>alert(1)</script>")
)

// newApp starts an apptest.App with uploads stored in a temporary folder,
// which it returns too.
func newApp(t *testing.T) (*apptest.App, *uploads.Service, string) {
	t.Helper()
	a := apptest.New(t)
	dir := t.TempDir()
	disk, err := uploads.NewDisk(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = disk.Close() })
	svc := uploads.NewService(a.DB, disk, a.Accounts, a.Logger)
	a.Worker.Handle(uploads.DeleteFilesKind, svc.DeleteFiles())
	return a, svc, dir
}

// runJobs runs the queued jobs, such as deleting files.
func runJobs(t *testing.T, a *apptest.App) {
	t.Helper()
	for {
		ran, err := a.Worker.RunOne(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !ran {
			return
		}
	}
}

// stored returns the number of files in dir.
func stored(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

type response struct {
	Status int
	Header http.Header
	Body   string
}

// postFile uploads content as filename to path, as c.
func postFile(t *testing.T, a *apptest.App, c *http.Client, path, filename string, content []byte) response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(content)
	_ = mw.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, a.URL(path), &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return do(t, c, req)
}

// get requests path as c.
func get(t *testing.T, a *apptest.App, c *http.Client, path string) response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, a.URL(path), nil)
	if err != nil {
		t.Fatal(err)
	}
	return do(t, c, req)
}

func do(t *testing.T, c *http.Client, req *http.Request) response {
	t.Helper()
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{Status: res.StatusCode, Header: res.Header, Body: string(b)}
}

func TestSave(t *testing.T) {
	t.Parallel()
	a, svc, dir := newApp(t)
	ctx := t.Context()
	owner := a.SignIn(a.Client(), "ada@example.com").ID

	u, err := svc.Save(ctx, owner, `C:\Users\ada\..\holiday.png`, bytes.NewReader(png))
	if err != nil {
		t.Fatal(err)
	}
	if u.Filename != "holiday.png" || u.ContentType != "image/png" || u.Size != int64(len(png)) || !u.Inline() {
		t.Errorf("saved %+v", u)
	}

	tooBig := append(bytes.Clone(png), make([]byte, uploads.MaxSize)...)
	for name, content := range map[string][]byte{"html": html, "empty": nil, "too big": tooBig} {
		var errs validate.Errors
		if _, err := svc.Save(ctx, owner, "x.png", bytes.NewReader(content)); !errors.As(err, &errs) || errs["file"] == "" {
			t.Errorf("%s: err = %v, want a message about the file", name, err)
		}
	}
	if n := stored(t, dir); n != 1 {
		t.Errorf("%d files stored, want only the PNG", n)
	}
}

func TestDiskRejectsOtherKeys(t *testing.T) {
	t.Parallel()
	disk, err := uploads.NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = disk.Close() }()
	for _, key := range []string{"../escape", "a/b", "", strings.Repeat("A", 27)} {
		if _, err := disk.Put(t.Context(), key, bytes.NewReader(png)); err == nil {
			t.Errorf("Put accepted key %q", key)
		}
	}
}

// TestDeleteAccount checks that deleting an account deletes its files.
func TestDeleteAccount(t *testing.T) {
	t.Parallel()
	a, svc, dir := newApp(t)
	ada := a.SignIn(a.Client(), "ada@example.com")
	for range 2 {
		if _, err := svc.Save(t.Context(), ada.ID, "f.pdf", bytes.NewReader(pdf)); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Accounts.DeleteUser(t.Context(), ada.ID); err != nil {
		t.Fatal(err)
	}
	runJobs(t, a)
	if n := stored(t, dir); n != 0 {
		t.Errorf("%d files left after deleting the account", n)
	}
}
