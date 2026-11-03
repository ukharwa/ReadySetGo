package main

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/db"
)

// seedPassword is the password of every seeded user. Seeding refuses to run
// outside dev, so it never reaches a real database.
const seedPassword = "dev-password-123"

// seed adds sample data for development. It is safe to run again: rows that
// already exist are left alone.
func seed(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, logger, err := setup(getenv, stdout)
	if err != nil {
		return err
	}
	if !cfg.Dev() {
		return errors.New("seed only runs with APP_ENV=dev")
	}
	database, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB(database, logger)
	if err := migrateOnStart(ctx, cfg, database, logger); err != nil {
		return err
	}
	svc, err := newServices(cfg, database, logger)
	if err != nil {
		return err
	}

	for _, u := range []struct{ email, name, role string }{
		{"admin@example.com", "Ada Admin", auth.RoleAdmin},
		{"user@example.com", "Uma User", auth.RoleUser},
	} {
		created, err := svc.accounts.CreateUser(ctx, u.email, seedPassword, u.role, true)
		if err == nil {
			_, err = svc.accounts.UpdateProfile(ctx, created, u.name, auth.DefaultTimeZone)
		}
		switch {
		case errors.Is(err, db.ErrConflict):
			logger.Info("seed user exists", "email", u.email)
		case err != nil:
			return err
		default:
			logger.Info("seed user created", "email", u.email, "role", u.role, "password", seedPassword)
		}
	}
	return nil
}
