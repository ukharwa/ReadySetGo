package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/pressly/goose/v3"
)

// Migrations returns a goose provider for the migrations embedded in the
// binary.
func (d *DB) Migrations() (*goose.Provider, error) {
	opts, err := gooseOptions()
	if err != nil {
		return nil, err
	}
	p, err := goose.NewProvider(gooseDialect, d.Write, migrationsFS, opts...)
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	return p, nil
}

// MigrateUp applies every pending migration, logging each one.
func (d *DB) MigrateUp(ctx context.Context, logger *slog.Logger) error {
	p, err := d.Migrations()
	if err != nil {
		return err
	}
	results, err := p.Up(ctx)
	for _, r := range results {
		logger.InfoContext(ctx, "migration applied",
			"version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	if err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
