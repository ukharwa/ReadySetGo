package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const migrateUsage = "usage: migrate up | down | status | create NAME"

// migrate manages the database schema. "create" only writes files and needs
// no configuration; the others connect to DATABASE_URL.
func migrate(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	action := fs.Arg(0)
	switch {
	case action == "create" && fs.NArg() == 2:
		return createMigration(migrationDirs(), fs.Arg(1), stdout)
	case (action == "up" || action == "down" || action == "status") && fs.NArg() == 1:
	default:
		return errors.New(migrateUsage)
	}

	cfg, logger, err := setup(getenv, stdout)
	if err != nil {
		return err
	}
	database, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB(database, logger)

	ctx, cancel := context.WithTimeout(ctx, migrateTimeout)
	defer cancel()

	if action == "up" {
		return database.MigrateUp(ctx, logger)
	}
	p, err := database.Migrations()
	if err != nil {
		return err
	}
	if action == "down" {
		r, err := p.Down(ctx)
		if err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
		logger.Info("migration rolled back", "version", r.Source.Version, "file", r.Source.Path)
		return nil
	}

	statuses, err := p.Status(ctx)
	if err != nil {
		return fmt.Errorf("migration status: %w", err)
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "VERSION\tSTATE\tAPPLIED AT\tFILE")
	for _, s := range statuses {
		applied := "-"
		if !s.AppliedAt.IsZero() {
			applied = s.AppliedAt.UTC().Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", s.Source.Version, s.State, applied, filepath.Base(s.Source.Path))
	}
	return w.Flush()
}

// migrationDirs are the folders `migrate create` writes to, relative to the
// repository root.
func migrationDirs() []string {
	return []string{
		// readysetgo:begin db=sqlite
		"migrations/sqlite",
		// readysetgo:end
		// readysetgo:begin db=postgres
		"migrations/postgres",
		// readysetgo:end
	}
}

var migrationName = regexp.MustCompile(`^[a-z0-9_]+$`)

const migrationTemplate = `-- +goose Up


-- +goose Down

`

// createMigration writes an empty migration with the next sequential version
// into each of dirs.
func createMigration(dirs []string, name string, stdout io.Writer) error {
	if !migrationName.MatchString(name) {
		return fmt.Errorf("migration name %q: use lowercase letters, digits and underscores", name)
	}
	next := 1
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read %s (run from the repository root): %w", dir, err)
		}
		for _, e := range entries {
			prefix, _, ok := strings.Cut(e.Name(), "_")
			if v, err := strconv.Atoi(prefix); ok && err == nil && strings.HasSuffix(e.Name(), ".sql") {
				next = max(next, v+1)
			}
		}
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, fmt.Sprintf("%05d_%s.sql", next, name))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // a source file, readable like the rest of the repository
		if err != nil {
			return fmt.Errorf("create migration: %w", err)
		}
		_, err = f.WriteString(migrationTemplate)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		_, _ = fmt.Fprintln(stdout, "created", path)
	}
	return nil
}
