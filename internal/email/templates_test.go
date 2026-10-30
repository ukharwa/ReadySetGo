package email

import (
	"testing"
	"testing/fstest"
)

func TestTemplates(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"welcome.txt": {Data: []byte("Subject: Welcome to {{.App}}\n\nHello {{.Name}}.\n")},
		"broken.txt":  {Data: []byte("No subject here.\n")},
	}
	tmpl, err := ParseTemplates(fsys, "*.txt")
	if err != nil {
		t.Fatal(err)
	}

	m, err := tmpl.Message("welcome.txt", "ada@example.com", map[string]string{"App": "Kick", "Name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	want := Message{To: "ada@example.com", Subject: "Welcome to Kick", Body: "Hello Ada.\n"}
	if m != want {
		t.Errorf("Message = %+v, want %+v", m, want)
	}

	if _, err := tmpl.Message("broken.txt", "ada@example.com", nil); err == nil {
		t.Error("template without a Subject line rendered")
	}
	if _, err := tmpl.Message("welcome.txt", "ada@example.com", map[string]string{"App": "Kick"}); err == nil {
		t.Error("missing template data went unnoticed")
	}
}
