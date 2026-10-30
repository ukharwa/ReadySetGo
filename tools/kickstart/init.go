package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

func runInit(args []string) error {
	o, err := parseOptions(flag.NewFlagSet("init", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if err := o.validate(); err != nil {
		return err
	}
	if err := checkGit("."); err != nil {
		return err
	}
	if err := initProject(".", o); err != nil {
		return err
	}
	fmt.Printf("Created %s (%s, database: %s). Running go mod tidy…\n", o.name, o.module, o.db)
	if err := runIn(".", "go", "mod", "tidy"); err != nil {
		return err
	}
	fmt.Println("\nDone. Review the result with `git status`, then:")
	fmt.Println("  cp .env.example .env")
	if o.db == "postgres" {
		fmt.Println("  make services   # start Postgres")
	}
	fmt.Println("  make test")
	fmt.Println("  make run")
	return nil
}

// checkGit refuses to run on uncommitted changes, so the result can always be
// reviewed with `git diff` and undone with `git checkout`.
func checkGit(root string) error {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return nil // not a git checkout (template-check's temporary copy)
	}
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if len(bytes.TrimSpace(out)) > 0 {
		return errors.New("the working tree has uncommitted changes; commit or stash them first")
	}
	return nil
}

// initProject rewrites the tree at root for o. It computes every output
// before touching the disk, so a mistake in the template (an unbalanced
// marker, say) leaves the tree unchanged.
func initProject(root string, o options) error {
	files, err := listFiles(root)
	if err != nil {
		return err
	}
	o.dialectDirs = dialectDirs(files, o.db)

	type output struct {
		path string
		data []byte
		mode fs.FileMode
	}
	var outputs []output
	sources := map[string]string{}
	for _, rel := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		newRel, newData, keep, err := transform(rel, data, o)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if !keep {
			continue
		}
		if prev, dup := sources[newRel]; dup {
			return fmt.Errorf("%s and %s would both become %s", prev, rel, newRel)
		}
		sources[newRel] = rel
		outputs = append(outputs, output{newRel, newData, info.Mode().Perm()})
	}

	for _, rel := range files {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	for _, out := range outputs {
		path := filepath.Join(root, filepath.FromSlash(out.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, out.data, out.mode); err != nil {
			return err
		}
	}
	return removeEmptyDirs(root)
}

// skipDirs are never part of the template's content.
var skipDirs = []string{".git", "bin"}

func listFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir() && slices.Contains(skipDirs, rel):
			return filepath.SkipDir
		case d.IsDir(), rel == ".env":
			return nil
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

func removeEmptyDirs(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() && slices.Contains(skipDirs, filepath.ToSlash(rel)) {
			return filepath.SkipDir
		}
		if d.IsDir() && rel != "." {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	slices.Reverse(dirs) // children before parents
	for _, dir := range dirs {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			if err := os.Remove(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

// transform decides whether one template file belongs in the project and,
// if so, returns its new path and contents.
func transform(rel string, data []byte, o options) (string, []byte, bool, error) {
	if !keepPath(rel, o) {
		return "", nil, false, nil
	}
	newRel := movePath(rel, o)
	if bytes.IndexByte(data, 0) >= 0 {
		return newRel, data, true, nil // binary: copy as is
	}

	text := string(data)
	if strings.HasSuffix(rel, ".go") {
		keep, rest := applyBuildConstraint(text, o)
		if !keep {
			return "", nil, false, nil
		}
		text = rest
	}
	text, err := applyMarkers(text, o)
	if err != nil {
		return "", nil, false, err
	}
	text = rewrite(rel, text, o)

	if strings.HasSuffix(newRel, ".go") {
		formatted, err := format.Source([]byte(text))
		if err != nil {
			return "", nil, false, fmt.Errorf("gofmt after init: %w", err)
		}
		text = string(formatted)
	}
	return newRel, []byte(text), true, nil
}

// A dialect folder is a folder named after a database, holding what only
// that database needs (migrations/postgres/, internal/jobs/sqlite/). Init
// deletes the other database's folders and flattens the chosen one's into
// their parents.

// keepPath reports whether rel survives init, judged by its path alone.
func keepPath(rel string, o options) bool {
	dirs := strings.Split(path.Dir(rel), "/")
	switch {
	case dirs[0] == "tools", rel == ".github/workflows/template.yml":
		return false // template-only
	case slices.Contains(dirs, otherDB(o.db)):
		return false
	case o.db == "postgres" && path.Dir(rel) == "internal/store" && strings.HasSuffix(rel, ".go"):
		return false // SQLite's generated code; Postgres's moves up to replace it
	}
	return true
}

// movePath flattens the chosen database's folders and renames the command.
func movePath(rel string, o options) string {
	dirs := strings.Split(path.Dir(rel), "/")
	dirs = slices.DeleteFunc(dirs, func(d string) bool { return d == o.db })
	if len(dirs) >= 2 && dirs[0] == "cmd" && dirs[1] == "kickstart" {
		dirs[1] = o.name
	}
	return path.Join(append(dirs, path.Base(rel))...)
}

// dialectDirs lists the chosen database's folders found in files, deepest
// first, so references to them can be rewritten.
func dialectDirs(files []string, db string) []string {
	var dirs []string
	for _, f := range files {
		for d := path.Dir(f); d != "."; d = path.Dir(d) {
			if path.Base(d) == db && !slices.Contains(dirs, d) {
				dirs = append(dirs, d)
			}
		}
	}
	slices.SortFunc(dirs, func(a, b string) int { return len(b) - len(a) })
	return dirs
}

// applyBuildConstraint handles the build tags that keep each database's Go
// files apart before init: `//go:build postgres` and `//go:build !postgres`.
// The file is dropped if it belongs to the other database; otherwise the
// constraint line is removed. Files with other constraints are left alone.
func applyBuildConstraint(text string, o options) (keep bool, rest string) {
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			break
		}
		var forPostgres bool
		switch trimmed {
		case "//go:build postgres":
			forPostgres = true
		case "//go:build !postgres":
			forPostgres = false
		default:
			continue
		}
		if forPostgres != (o.db == "postgres") {
			return false, ""
		}
		end := i + 1
		if end < len(lines) && strings.TrimSpace(lines[end]) == "" {
			end++ // the blank line a build constraint must be followed by
		}
		return true, strings.Join(slices.Delete(lines, i, end), "")
	}
	return true, text
}

// markerLine matches a marker comment on a line of its own, in any of the
// comment styles the template uses: //, #, -- or <!-- -->.
var markerLine = regexp.MustCompile(`^\s*(?://|#|--|<!--)\s*kickstart:(begin|end)\b\s*(\S*?)\s*(?:-->)?\s*$`)

var blankLines = regexp.MustCompile(`\n{3,}`)

// applyMarkers keeps or drops each `kickstart:begin <condition>` …
// `kickstart:end` block and removes the marker lines. Blocks may nest.
func applyMarkers(text string, o options) (string, error) {
	var out strings.Builder
	var stack []bool
	changed := false
	for i, line := range strings.SplitAfter(text, "\n") {
		m := markerLine.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
		if m == nil {
			if !slices.Contains(stack, false) {
				out.WriteString(line)
			}
			continue
		}
		changed = true
		if m[1] == "begin" {
			keep, err := evalCondition(m[2], o)
			if err != nil {
				return "", fmt.Errorf("line %d: %w", i+1, err)
			}
			stack = append(stack, keep)
			continue
		}
		if m[2] != "" {
			return "", fmt.Errorf("line %d: kickstart:end takes no condition", i+1)
		}
		if len(stack) == 0 {
			return "", fmt.Errorf("line %d: kickstart:end without a matching begin", i+1)
		}
		stack = stack[:len(stack)-1]
	}
	if len(stack) > 0 {
		return "", errors.New("kickstart:begin without a matching end")
	}
	if !changed {
		return text, nil
	}
	// Removing blocks can leave runs of blank lines; collapse them.
	s := blankLines.ReplaceAllString(out.String(), "\n\n")
	return strings.TrimRight(s, "\n") + "\n", nil
}

func evalCondition(cond string, o options) (bool, error) {
	if cond == "template" {
		return false, nil
	}
	key, val, _ := strings.Cut(cond, "=")
	switch {
	case key == "db" && slices.Contains(databases, val):
		return val == o.db, nil
	case key == "ui" && slices.Contains(uiModes, val):
		return val == o.ui, nil
	case key == "module" && slices.Contains(allModules, val):
		return slices.Contains(o.modules, val), nil
	}
	return false, fmt.Errorf("unknown marker condition %q", cond)
}

var templateName = regexp.MustCompile(`\bkickstart\b`)

// rewrite updates references to paths that moved and to the template's name.
// DECISIONS.md keeps "kickstart": there it names the template itself.
func rewrite(rel, text string, o options) string {
	for _, d := range o.dialectDirs {
		text = regexp.MustCompile(regexp.QuoteMeta(d)+`\b`).ReplaceAllLiteralString(text, path.Dir(d))
	}
	text = strings.ReplaceAll(text, templateModule, o.module)
	if rel != "DECISIONS.md" {
		text = templateName.ReplaceAllString(text, o.name)
	}
	return text
}

func otherDB(db string) string {
	if db == "postgres" {
		return "sqlite"
	}
	return "postgres"
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
