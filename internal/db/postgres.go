//go:build postgres

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	migrations "github.com/ukharwa/kickstart/migrations/postgres"
)

const gooseDialect = goose.DialectPostgres

var migrationsFS = migrations.FS

// gooseOptions takes a Postgres advisory lock around every migration run, so
// instances starting at the same time apply each migration exactly once.
// Waiting instances retry every second for up to 10 minutes (goose's default
// is every 5 seconds for 5 minutes), so they can outwait a long migration.
func gooseOptions() ([]goose.ProviderOption, error) {
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 600))
	if err != nil {
		return nil, fmt.Errorf("migration lock: %w", err)
	}
	return []goose.ProviderOption{goose.WithSessionLocker(locker)}, nil
}

// Open connects to the Postgres database at opts.URL. Read and Write share one
// pool unless opts.ReadURL names a read replica, which then gets a pool of its
// own.
func Open(ctx context.Context, opts Options) (*DB, error) {
	write, err := openPool(ctx, opts.URL, opts)
	if err != nil {
		return nil, err
	}
	if opts.ReadURL == "" {
		return &DB{Read: write, Write: write}, nil
	}
	read, err := openPool(ctx, opts.ReadURL, opts)
	if err != nil {
		_ = write.Close()
		return nil, err
	}
	return &DB{Read: read, Write: write}, nil
}

func openPool(ctx context.Context, url string, opts Options) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		// The error can quote the URL, password included, so it isn't wrapped.
		return nil, errors.New("parse postgres URL: invalid format")
	}
	pool := stdlib.OpenDB(*cfg, stdlib.OptionAfterConnect(scanTimesAsUTC))
	pool.SetMaxOpenConns(opts.MaxOpenConns)
	pool.SetMaxIdleConns(opts.MaxOpenConns)
	pool.SetConnMaxIdleTime(opts.ConnMaxIdleTime)
	pool.SetConnMaxLifetime(opts.ConnMaxLifetime)
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	return pool, nil
}

// scanTimesAsUTC makes timestamptz values come back in UTC rather than the
// server process's local time zone.
func scanTimesAsUTC(_ context.Context, conn *pgx.Conn) error {
	conn.TypeMap().RegisterType(&pgtype.Type{
		Name:  "timestamptz",
		OID:   pgtype.TimestamptzOID,
		Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC},
	})
	return nil
}

// Close closes the pools.
func (d *DB) Close() error {
	if d.Read == d.Write {
		return d.Write.Close()
	}
	return errors.Join(d.Read.Close(), d.Write.Close())
}

func isUniqueViolation(err error) bool {
	var e *pgconn.PgError
	return errors.As(err, &e) && e.Code == "23505" // unique_violation
}
