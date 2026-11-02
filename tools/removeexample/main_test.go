package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := map[string]string{
		"app.go": "package main\n\nimport (\n\t\"fmt\"\n\n\t// example:begin\n\t\"example.com/app/internal/notes\"\n\t// example:end\n)\n\n" +
			"type services struct {\n\tlimiter  int\n\t// example:begin\n\tnotes *notes.Service\n\t// example:end\n}\n\nvar _ = fmt.Sprint\n",
		"migrations/00007_notes.sql":  "-- example:begin\n-- +goose Up\nCREATE TABLE notes (id TEXT);\n-- example:end\n",
		"layout.html":                 "<nav>\n  <!-- example:begin -->\n  <a href=\"/notes\">Notes</a>\n  <!-- example:end -->\n</nav>\n",
		"Makefile":                    "a:\n\techo a\n\n# example:begin\nremove-example:\n\tgo run ./tools/removeexample\n# example:end\n\nb:\n\techo b\n",
		"internal/notes/notes.go":     "package notes\n",
		"tools/removeexample/main.go": "package main\n// example:begin is mentioned here, not used\n",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := run(dir, io.Discard); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"app.go":      "package main\n\nimport (\n\t\"fmt\"\n)\n\ntype services struct {\n\tlimiter int\n}\n\nvar _ = fmt.Sprint\n",
		"layout.html": "<nav>\n</nav>\n",
		"Makefile":    "a:\n\techo a\n\nb:\n\techo b\n",
	}
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != content {
			t.Errorf("%s:\ngot  %q (%v)\nwant %q", name, got, err, content)
		}
	}
	for _, gone := range []string{"migrations/00007_notes.sql", "internal/notes", "tools"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still exists (%v)", gone, err)
		}
	}
}

func TestRemoveBlocksErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"# example:begin\nx\n", "x\n# example:end\n"} {
		if _, err := removeBlocks(in); err == nil {
			t.Errorf("no error for %q", in)
		}
	}
}
