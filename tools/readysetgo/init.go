package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
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
	if out, err := exec.CommandContext(context.Background(), "git", "rev-parse", "--short=12", "HEAD").Output(); err == nil {
		o.commit = strings.TrimSpace(string(out))
	}
	if err := initProject(".", o); err != nil {
		return err
	}
	fmt.Printf("Created %s (%s, database: %s). Running go mod tidy…\n", o.name, o.module, o.db)
	if err := tidyProject(".", o); err != nil {
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

// tidyProject drops the other database's driver, and the modules only it
// uses, from go.mod, then runs go mod tidy. goose's own go.mod requires both
// drivers, so once the template requires newer versions than goose does,
// tidy alone would keep the unused ones as indirect requirements rather than
// downgrade them. Tidy adds back anything a remaining package does need.
func tidyProject(dir string, o options) error {
	prefix := "github.com/jackc/" // pgx and its helpers
	if o.db == "postgres" {
		prefix = "modernc.org/" // the SQLite driver and its C runtime
	}
	cmd := exec.CommandContext(context.Background(), "go", "mod", "edit", "-json")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go mod edit -json: %w", err)
	}
	var mod struct{ Require []struct{ Path string } }
	if err := json.Unmarshal(out, &mod); err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	args := []string{"mod", "edit"}
	for _, r := range mod.Require {
		if strings.HasPrefix(r.Path, prefix) {
			args = append(args, "-droprequire="+r.Path)
		}
	}
	if len(args) > 2 {
		if err := runIn(dir, "go", args...); err != nil {
			return err
		}
	}
	return runIn(dir, "go", "mod", "tidy")
}

// checkGit refuses to run on uncommitted changes, so the result can always be
// reviewed with `git diff` and undone with `git checkout`.
func checkGit(root string) error {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return nil // not a git checkout (template-check's temporary copy)
	}
	out, err := exec.CommandContext(context.Background(), "git", "-C", root, "status", "--porcelain").Output()
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
	o.replacedDirs = replacedDirs(files, o.db)

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
	marked := strings.Contains(text, "readysetgo:begin")
	text, err := applyMarkers(text, o)
	if err != nil {
		return "", nil, false, err
	}
	if marked && strings.TrimSpace(text) == "" {
		return "", nil, false, nil // every line belonged to an unchosen option
	}
	text = rewrite(rel, text, o)
	if rel == "AGENTS.md" {
		text = recordInit(text, o)
	}

	if strings.HasSuffix(newRel, ".go") {
		text = dropRedundantAliases(text)
		text = joinLocalImports(text, o.module)
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
	case strings.HasPrefix(rel, "tools/readysetgo/"), rel == ".github/workflows/template.yml", rel == "LICENSE":
		return false // template-only; a project chooses its own licence
	case slices.Contains(dirs, otherDB(o.db)), strings.Contains(path.Base(rel), otherDB(o.db)):
		return false // the other database's folder or file (backup-postgres.sh)
	case !forUI(rel, o.ui):
		return false
	case len(dirs) >= 2 && dirs[0] == "internal" && slices.Contains(allModules, dirs[1]) && !slices.Contains(o.modules, dirs[1]):
		return false // an optional module that wasn't chosen
	case strings.HasSuffix(rel, ".go") && slices.Contains(o.replacedDirs, path.Dir(rel)):
		return false // the other database's version of code the chosen one's replaces
	}
	return true
}

// forUI reports whether rel is kept for the UI mode ui. Files named web.go
// or web_test.go, templates/ folders and internal/web/ serve HTML pages;
// files named api.go or api_test.go serve the JSON API. Everything else is
// shared.
func forUI(rel, ui string) bool {
	base := path.Base(rel)
	html := base == "web.go" || base == "web_test.go" || strings.HasPrefix(rel, "internal/web/") ||
		slices.Contains(strings.Split(path.Dir(rel), "/"), "templates")
	api := base == "api.go" || base == "api_test.go"
	return (!html || ui == "html") && (!api || ui == "api")
}

// movePath flattens the chosen database's folders and renames the command.
func movePath(rel string, o options) string {
	dirs := strings.Split(path.Dir(rel), "/")
	dirs = slices.DeleteFunc(dirs, func(d string) bool { return d == o.db })
	if len(dirs) >= 2 && dirs[0] == "cmd" && dirs[1] == "readysetgo" {
		dirs[1] = o.name
	}
	return path.Join(append(dirs, path.Base(rel))...)
}

// replacedDirs lists folders whose own Go files the chosen database's
// folder inside them replaces. The generated stores work this way:
// internal/auth/store/ holds SQLite's code and internal/auth/store/postgres/
// Postgres's, which moves up to take its place.
func replacedDirs(files []string, db string) []string {
	var dirs []string
	for _, f := range files {
		if d := path.Dir(f); path.Base(d) == db && strings.HasSuffix(f, ".go") && !slices.Contains(dirs, path.Dir(d)) {
			dirs = append(dirs, path.Dir(d))
		}
	}
	return dirs
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
var markerLine = regexp.MustCompile(`^\s*(?://|#|--|<!--)\s*readysetgo:(begin|end)\b\s*(\S*?)\s*(?:-->)?\s*$`)

var blankLines = regexp.MustCompile(`\n{3,}`)

// applyMarkers keeps or drops each `readysetgo:begin <condition>` …
// `readysetgo:end` block and removes the marker lines. Blocks may nest. A
// comma-separated list of conditions keeps the block if any of them holds.
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
			return "", fmt.Errorf("line %d: readysetgo:end takes no condition", i+1)
		}
		if len(stack) == 0 {
			return "", fmt.Errorf("line %d: readysetgo:end without a matching begin", i+1)
		}
		stack = stack[:len(stack)-1]
	}
	if len(stack) > 0 {
		return "", errors.New("readysetgo:begin without a matching end")
	}
	if !changed {
		return text, nil
	}
	// Removing blocks can leave runs of blank lines; collapse them.
	s := blankLines.ReplaceAllString(out.String(), "\n\n")
	return strings.TrimRight(s, "\n") + "\n", nil
}

func evalCondition(conds string, o options) (bool, error) {
	keep := false
	for cond := range strings.SplitSeq(conds, ",") {
		ok, err := evalOne(cond, o)
		if err != nil {
			return false, err
		}
		keep = keep || ok
	}
	return keep, nil
}

func evalOne(cond string, o options) (bool, error) {
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

// templateName matches the template's name in any letter case: readysetgo
// in code and paths, ReadySetGo in prose.
var templateName = regexp.MustCompile(`(?i)\breadysetgo\b`)

// redundantAlias matches an import whose alias is just its last path
// element, like `migrations ".../migrations"`: needed before init, when the
// folder was named after a database, and noise after it.
var redundantAlias = regexp.MustCompile(`(?m)^(\s*)(\w+) ("[^"\n]*/(\w+)")$`)

// joinLocalImports removes blank lines between the project's own imports.
// goimports wants one before each marker comment in an import block; once
// init removes the markers, those blank lines would split the group.
func joinLocalImports(text, module string) string {
	pattern := regexp.MustCompile(`(\t"` + regexp.QuoteMeta(module) + `/[^"\n]*"\n)\n+(\t"` + regexp.QuoteMeta(module) + `/)`)
	for {
		joined := pattern.ReplaceAllString(text, "$1$2")
		if joined == text {
			return text
		}
		text = joined
	}
}

func dropRedundantAliases(text string) string {
	return redundantAlias.ReplaceAllStringFunc(text, func(line string) string {
		m := redundantAlias.FindStringSubmatch(line)
		if m[2] != m[4] {
			return line // a real alias
		}
		return m[1] + m[3]
	})
}

// rewrite updates references to paths that moved and to the template's name.
// DECISIONS.md keeps the template's name: there it names the template itself.
// command is the make init command that produces o.
func (o options) command() string {
	modules := cmp.Or(strings.Join(o.modules, ","), "none")
	return fmt.Sprintf("make init NAME=%s MODULE=%s DB=%s UI=%s MODULES=%s", o.name, o.module, o.db, o.ui, modules)
}

// recordInit fills in AGENTS.md's record of how the project was made: the
// command, and the template commit it ran on, which later changes start
// from.
func recordInit(text string, o options) string {
	return strings.NewReplacer("INIT_COMMAND", o.command(), "INIT_COMMIT", cmp.Or(o.commit, "unknown")).Replace(text)
}

func rewrite(rel, text string, o options) string {
	for _, d := range o.dialectDirs {
		text = regexp.MustCompile(regexp.QuoteMeta(d)+`\b`).ReplaceAllLiteralString(text, path.Dir(d))
	}
	text = strings.ReplaceAll(text, templateImage, imageFor(o))
	text = strings.ReplaceAll(text, templateModule, o.module)
	if rel != "DECISIONS.md" {
		text = templateName.ReplaceAllString(text, o.name)
	}
	return text
}

// templateImage is the template's container image name.
const templateImage = "ghcr.io/ukharwa/readysetgo"

// imageFor names the project's image after its module: GitHub's container
// registry for a GitHub module, otherwise a plain local name (set APP_IMAGE).
func imageFor(o options) string {
	if repo, ok := strings.CutPrefix(o.module, "github.com/"); ok {
		return "ghcr.io/" + strings.ToLower(repo)
	}
	return o.name
}

func otherDB(db string) string {
	if db == "postgres" {
		return "sqlite"
	}
	return "postgres"
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
