//go:build !postgres

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pressly/goose/v3"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/ukharwa/kickstart/migrations/sqlite"
)

const gooseDialect = goose.DialectSQLite3

var migrationsFS = migrations.FS

func gooseOptions() ([]goose.ProviderOption, error) { return nil, nil }

// Open opens the SQLite database file at opts.URL, creating it and its
// folder if needed.
//
// SQLite allows one writer at a time, so writes go through a pool with a
// single connection whose transactions take the write lock up front
// (BEGIN IMMEDIATE): concurrent writers queue in Go instead of failing with
// SQLITE_BUSY. Reads use a separate pool of read-only connections, which WAL
// mode lets run alongside the writer.
func Open(ctx context.Context, opts Options) (*DB, error) {
	path := opts.URL
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database folder: %w", err)
	}

	params := url.Values{
		"_busy_timeout": {"5000"},
		"_journal_mode": {"WAL"},
		"_foreign_keys": {"1"},
		"_synchronous":  {"NORMAL"}, // safe in WAL mode; a power cut can lose the last commits
		"_time_format":  {"sqlite"},
		"_timezone":     {"UTC"},
	}

	writeParams := maps.Clone(params)
	writeParams.Set("_txlock", "immediate")
	write, err := sql.Open("sqlite", path+"?"+writeParams.Encode())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	write.SetConnMaxIdleTime(0)
	write.SetConnMaxLifetime(0)
	// The write pool connects first so the file exists and is in WAL mode
	// before any read-only connection opens it.
	if err := write.PingContext(ctx); err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	readParams := maps.Clone(params)
	readParams.Set("_query_only", "1")
	read, err := sql.Open("sqlite", path+"?"+readParams.Encode())
	if err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	n := max(4, runtime.GOMAXPROCS(0))
	read.SetMaxOpenConns(n)
	read.SetMaxIdleConns(n)
	read.SetConnMaxIdleTime(0)
	read.SetConnMaxLifetime(0)

	return &DB{Read: read, Write: write}, nil
}

// Close refreshes the query planner's statistics and closes both pools. When
// the last connection to the file closes, SQLite folds the write-ahead log
// back into the database file, so the file is complete on its own. (An
// explicit checkpoint here would briefly block writers in other processes
// whenever a command like `app backup` exits.)
func (d *DB) Close() error {
	readErr := d.Read.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, optimizeErr := d.Write.ExecContext(ctx, "PRAGMA optimize")
	writeErr := d.Write.Close()

	return errors.Join(readErr, optimizeErr, writeErr)
}

// Backup writes a consistent copy of the database to path, which must not
// exist yet. VACUUM INTO only reads the database, so other processes keep
// writing while it runs. It needs the write handle (read-only connections
// refuse it), so writes in this process wait: run it as `app backup`.
func (d *DB) Backup(ctx context.Context, path string) error {
	if _, err := d.Write.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return fmt.Errorf("vacuum into %s: %w", path, err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var e *sqlite.Error
	if !errors.As(err, &e) {
		return false
	}
	code := e.Code()
	return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}
