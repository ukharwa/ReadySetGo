package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// defaultTestDatabaseURL is the Postgres started by `make services`.
const defaultTestDatabaseURL = "postgres://kickstart:kickstart@localhost:5432/postgres?sslmode=disable"

// runCheck copies the working tree (uncommitted changes included) to a
// temporary folder, runs init there, and then checks the result the way a
// new project would be checked: tidy, formatting, vet, tests, lint, generated
// code, and leftovers from options that weren't chosen.
func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	keep := fs.Bool("keep", false, "keep the temporary project even if every check passes")
	o, err := parseOptions(fs, args)
	if err != nil {
		return err
	}
	o.name = "checkapp"
	o.module = "example.com/checkapp"
	if err := o.validate(); err != nil {
		return err
	}

	repo, err := os.Getwd()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "kickstart-check-")
	if err != nil {
		return err
	}
	failed := true
	defer func() {
		if failed || *keep {
			fmt.Println("project left in", tmp)
			return
		}
		_ = os.RemoveAll(tmp)
	}()

	fmt.Printf("==> checking db=%s ui=%s modules=%s in %s\n", o.db, o.ui, strings.Join(o.modules, ","), tmp)
	if err := copyWorkingTree(repo, tmp); err != nil {
		return fmt.Errorf("copy working tree: %w", err)
	}
	if err := initProject(tmp, o); err != nil {
		return fmt.Errorf("init: %w", err)
	}

	if os.Getenv("TEST_DATABASE_URL") == "" {
		if err := os.Setenv("TEST_DATABASE_URL", defaultTestDatabaseURL); err != nil {
			return err
		}
	}
	steps := [][]string{
		{"go", "mod", "tidy"}, // as runInit does
		{"git", "init", "-q"},
		{"git", "add", "-A"},
		{"git", "-c", "user.name=check", "-c", "user.email=check@example.com", "commit", "-q", "-m", "init"},
		{"go", "vet", "./..."},
		{"go", "test", "-race", "-count=1", "./..."},
	}
	if _, err := exec.LookPath("golangci-lint"); err == nil {
		steps = append(steps, []string{"golangci-lint", "run", "./..."})
	}
	if sqlc := filepath.Join(repo, "bin", "sqlc"); fileExists(sqlc) {
		steps = append(steps, []string{sqlc, "generate"})
	}
	for _, step := range steps {
		fmt.Println("==>", strings.Join(step, " "))
		start := time.Now()
		if err := runIn(tmp, step[0], step[1:]...); err != nil {
			return err
		}
		fmt.Printf("    ok (%s)\n", time.Since(start).Round(time.Millisecond))
	}

	fmt.Println("==> files unchanged by tidy, gofmt and sqlc")
	if err := runIn(tmp, "go", "mod", "tidy"); err != nil {
		return err
	}
	if err := runIn(tmp, "gofmt", "-w", "."); err != nil {
		return err
	}
	out, err := exec.Command("git", "-C", tmp, "status", "--porcelain").Output()
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(out)) > 0 {
		return fmt.Errorf("these files changed after init (run the step yourself to see why):\n%s", out)
	}

	fmt.Println("==> no leftovers from unchosen options")
	if problems := leftovers(tmp, o); len(problems) > 0 {
		return fmt.Errorf("leftovers found:\n  %s", strings.Join(problems, "\n  "))
	}

	failed = false
	fmt.Println("==> all checks passed")
	return nil
}

// copyWorkingTree copies every file git would consider part of the project:
// tracked files and untracked files that aren't ignored.
func copyWorkingTree(repo, dst string) error {
	out, err := exec.Command("git", "-C", repo, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return fmt.Errorf("git ls-files: %w", err)
	}
	for rel := range strings.SplitSeq(strings.TrimRight(string(out), "\x00"), "\x00") {
		src := filepath.Join(repo, rel)
		info, err := os.Stat(src)
		if errors.Is(err, fs.ErrNotExist) {
			continue // deleted but not yet committed
		} else if err != nil {
			return err
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// leftovers lists anything in the project that belongs to an option that
// wasn't chosen, or to the template itself. DECISIONS.md is exempt from the
// name checks because it records the alternatives that were considered.
func leftovers(root string, o options) []string {
	otherDB := regexp.MustCompile(`(?i)\bpostgres(ql)?\b|\bpgx\b`)
	if o.db == "postgres" {
		otherDB = regexp.MustCompile(`(?i)\bsqlite3?\b|modernc\.org`)
	}
	var problems []string
	report := func(where, what string) { problems = append(problems, where+": "+what) }

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() && rel == ".git" {
			return filepath.SkipDir
		}
		if otherDB.MatchString(rel) || templateName.MatchString(rel) {
			report(rel, "path names an unchosen option or the template")
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			return err
		}
		exempt := rel == "DECISIONS.md" || rel == "go.sum"
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(nil, 1<<20)
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			where := fmt.Sprintf("%s:%d", rel, n)
			switch {
			case strings.Contains(line, "kickstart:begin"), strings.Contains(line, "kickstart:end"):
				report(where, "init marker left behind")
			case strings.HasPrefix(line, "//go:build postgres"), strings.HasPrefix(line, "//go:build !postgres"):
				report(where, "database build constraint left behind")
			case !exempt && otherDB.MatchString(line):
				report(where, "mentions the database that wasn't chosen: "+strings.TrimSpace(line))
			case !exempt && templateName.MatchString(line):
				report(where, "mentions the template's name: "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	return problems
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
