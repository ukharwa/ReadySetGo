// Package db opens the application's database and provides what every
// feature shares: separate read and write handles, transactions, IDs,
// timestamps and driver-independent errors.
//
// Database-specific code lives in this package's dialect file and in
// migrations/; nothing else knows which database is in use.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Errors returned by Err, so callers never depend on a driver's error types.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict") // a unique constraint was violated
)

// DB holds separate handles for reads and writes. Queries that only read use
// Read; everything else, including every transaction, uses Write.
type DB struct {
	Read  *sql.DB
	Write *sql.DB
}

// Options configures Open.
type Options struct {
	URL string // DATABASE_URL
	// kickstart:begin db=postgres
	ReadURL         string // DATABASE_READ_URL: optional read replica
	MaxOpenConns    int
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration
	// kickstart:end
}

// Ping checks that both handles can reach the database.
func (d *DB) Ping(ctx context.Context) error {
	if err := d.Write.PingContext(ctx); err != nil {
		return fmt.Errorf("ping write: %w", err)
	}
	if d.Read != d.Write {
		if err := d.Read.PingContext(ctx); err != nil {
			return fmt.Errorf("ping read: %w", err)
		}
	}
	return nil
}

// InTx runs fn in a transaction on the write handle. It commits if fn returns
// nil and rolls back otherwise.
func (d *DB) InTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// Err translates driver errors into ErrNotFound or ErrConflict, keeping the
// original error wrapped. Other errors are returned unchanged.
func Err(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case isUniqueViolation(err):
		return fmt.Errorf("%w: %w", ErrConflict, err)
	default:
		return err
	}
}

// NewID returns a new UUIDv7. Its time-ordered prefix keeps index inserts in
// order, and unlike sequential IDs it can't be guessed.
func NewID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// Now returns the current time as the database stores it: UTC, with
// microsecond precision, so values read back compare equal on every
// database.
func Now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
