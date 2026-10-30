package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/ukharwa/kickstart/internal/config"
	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/logging"
)

// migrateTimeout bounds a migration run. It is generous because adding an
// index to a large table can take minutes.
const migrateTimeout = 10 * time.Minute

// setup loads the configuration and builds the logger that every subcommand
// starts with.
func setup(getenv func(string) string, stdout io.Writer) (config.Config, *slog.Logger, error) {
	cfg, err := config.Load(getenv)
	if err != nil {
		return config.Config{}, nil, err
	}
	logger := logging.New(stdout, cfg.LogFormat, cfg.LogLevel)
	slog.SetDefault(logger)
	return cfg, logger, nil
}

// openDB opens the database described by cfg.
func openDB(ctx context.Context, cfg config.Config) (*db.DB, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d, err := db.Open(ctx, db.Options{
		URL: cfg.DatabaseURL,
		// kickstart:begin db=postgres
		ReadURL:         cfg.DatabaseReadURL,
		MaxOpenConns:    cfg.DBMaxOpenConns,
		ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
		// kickstart:end
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return d, nil
}

// closeDB closes d, logging rather than returning the error because it runs
// as a deferred call on the way out.
func closeDB(d *db.DB, logger *slog.Logger) {
	if err := d.Close(); err != nil {
		logger.Error("close database", "err", err)
		return
	}
	logger.Info("database closed")
}
