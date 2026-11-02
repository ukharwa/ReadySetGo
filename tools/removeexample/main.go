// Command removeexample deletes the notes example: the internal/notes
// folder, every block between example:begin and example:end markers (the
// example's migration, sqlc entries, routes, nav link, documentation and
// this command's Make target), any file those blocks leave empty, and this
// command itself. Run it with `make remove-example`.
//
// Do it before the first deploy. Once the notes migration has run on a
// database you keep, write a new migration that drops the table instead.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
)

const self = "tools/removeexample"

// marker matches an example marker on a line of its own, in any of the
// comment styles the project uses: //, #, -- or <!-- -->.
var marker = regexp.MustCompile(`^\s*(?://|#|--|<!--)\s*example:(begin|end)\s*(?:-->)?\s*$`)

var blankLines = regexp.MustCompile(`\n{3,}`)

// skipDirs hold no project sources.
var skipDirs = []string{".git", "bin", "data", "backups", "node_modules", "tmp"}

func main() {
	if err := run(".", os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "remove-example:", err)
		os.Exit(1)
	}
}

func run(dir string, out io.Writer) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	changed := 0
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (slices.Contains(skipDirs, d.Name()) || p == self):
			return fs.SkipDir
		case !d.Type().IsRegular():
			return nil
		}
		data, err := root.ReadFile(p)
		if err != nil || !bytes.Contains(data, []byte("example:begin")) {
			return err
		}
		text, err := removeBlocks(string(data))
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		changed++
		if strings.TrimSpace(text) == "" {
			return root.Remove(p)
		}
		if path.Ext(p) == ".go" {
			b, err := format.Source([]byte(text))
			if err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			text = string(b)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return root.WriteFile(p, []byte(text), info.Mode().Perm())
	})
	if err != nil {
		return err
	}

	for _, p := range []string{"internal/notes", self} {
		if err := root.RemoveAll(p); err != nil {
			return err
		}
	}
	_ = root.Remove("tools") // if nothing else is in it

	_, err = fmt.Fprintf(out, "Removed internal/notes and the example's lines in %d files.\n"+
		"If the notes migration ran on your development database, recreate that database.\n", changed)
	return err
}

// removeBlocks drops every example block, markers included. Blocks may
// nest.
func removeBlocks(text string) (string, error) {
	var out strings.Builder
	depth := 0
	for i, line := range strings.SplitAfter(text, "\n") {
		m := marker.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
		switch {
		case m == nil:
			if depth == 0 {
				out.WriteString(line)
			}
		case m[1] == "begin":
			depth++
		case depth == 0:
			return "", fmt.Errorf("line %d: example:end without a matching begin", i+1)
		default:
			depth--
		}
	}
	if depth > 0 {
		return "", errors.New("example:begin without a matching end")
	}
	// Removing blocks can leave runs of blank lines; collapse them.
	return blankLines.ReplaceAllString(out.String(), "\n\n"), nil
}
