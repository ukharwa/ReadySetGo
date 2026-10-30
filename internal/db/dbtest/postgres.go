//go:build postgres

package dbtest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver

	"github.com/ukharwa/kickstart/internal/db"
	migrations "github.com/ukharwa/kickstart/migrations/postgres"
)

// URL returns the URL of a new database for this test, dropped when the test
// ends. It connects to the server named by TEST_DATABASE_URL.
//
// The first call migrates a template database, named after a hash of the
// migrations so it is rebuilt when they change. Each test then gets a copy
// (CREATE DATABASE … TEMPLATE), which takes milliseconds, so tests stay
// isolated and can run in parallel, even across packages.
func URL(t testing.TB) string {
	t.Helper()
	server := os.Getenv("TEST_DATABASE_URL")
	if server == "" {
		t.Fatal("TEST_DATABASE_URL is not set; start Postgres with `make services` and see .env.example")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	admin, err := sql.Open("pgx", server)
	if err != nil {
		t.Fatalf("connect to test server: %v", err)
	}
	defer func() { _ = admin.Close() }()

	template, err := ensureTemplate(ctx, admin, server)
	if err != nil {
		t.Fatalf("prepare template database: %v", err)
	}

	name := "test_" + rand.Text()[:16]
	if err := ddl(ctx, admin, "CREATE DATABASE "+ident(name)+" TEMPLATE "+ident(template)); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		admin, err := sql.Open("pgx", server)
		if err != nil {
			t.Errorf("drop test database: %v", err)
			return
		}
		defer func() { _ = admin.Close() }()
		if err := ddl(ctx, admin, "DROP DATABASE "+ident(name)+" WITH (FORCE)"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})
	return withDatabase(server, name)
}

// ensureTemplate returns the name of a migrated template database, creating
// it if needed. An advisory lock serialises creation across test processes.
func ensureTemplate(ctx context.Context, admin *sql.DB, server string) (string, error) {
	sum, err := migrationsHash()
	if err != nil {
		return "", err
	}
	name := "test_template_" + sum

	conn, err := admin.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	const lockID = 7420533213 // arbitrary, shared by every test process
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return "", fmt.Errorf("lock: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", lockID) }()

	var exists bool
	if err := conn.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return name, nil
	}

	// Build under a temporary name and rename when complete, so a run that
	// crashes midway never leaves a half-migrated template behind.
	building := name + "_building"
	if err := ddl(ctx, conn, "DROP DATABASE IF EXISTS "+ident(building)+" WITH (FORCE)"); err != nil {
		return "", err
	}
	if err := ddl(ctx, conn, "CREATE DATABASE "+ident(building)); err != nil {
		return "", err
	}
	d, err := db.Open(ctx, db.Options{URL: withDatabase(server, building), MaxOpenConns: 2})
	if err != nil {
		return "", err
	}
	migrateErr := d.MigrateUp(ctx, slog.New(slog.DiscardHandler))
	if err := d.Close(); err != nil {
		return "", err
	}
	if migrateErr != nil {
		return "", migrateErr
	}
	if err := ddl(ctx, conn, "ALTER DATABASE "+ident(building)+" RENAME TO "+ident(name)); err != nil {
		return "", err
	}
	return name, nil
}

// migrationsHash identifies the current set of migrations.
func migrationsHash() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(migrations.FS, ".", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := fs.ReadFile(migrations.FS, path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00", path, b)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hash migrations: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// ident quotes a database name for use in SQL.
func ident(name string) string { return pgx.Identifier{name}.Sanitize() }

// ddl runs a statement about databases. Database names can't be query
// parameters, so stmt is built from names quoted with ident; they come from
// this file (random or hashed) anyway.
func ddl(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, stmt string) error {
	_, err := q.ExecContext(ctx, stmt) //nolint:gosec // see above
	return err
}

// withDatabase returns server with its database name replaced.
func withDatabase(server, name string) string {
	u, err := url.Parse(server)
	if err != nil {
		return server
	}
	u.Path = "/" + name
	return u.String()
}
