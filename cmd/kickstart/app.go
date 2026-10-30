package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/ukharwa/kickstart/internal/auth"
	"github.com/ukharwa/kickstart/internal/config"
	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/email"
	"github.com/ukharwa/kickstart/internal/jobs"
	"github.com/ukharwa/kickstart/internal/logging"
	"github.com/ukharwa/kickstart/internal/ratelimit"

	// kickstart:begin module=workspaces
	"github.com/ukharwa/kickstart/internal/workspaces"
	// kickstart:end
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

// migrateOnStart applies pending migrations if cfg asks for it.
func migrateOnStart(ctx context.Context, cfg config.Config, d *db.DB, logger *slog.Logger) error {
	if !cfg.MigrateOnStart {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, migrateTimeout)
	defer cancel()
	return d.MigrateUp(ctx, logger)
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

// services are the features' services, built once and shared by every
// subcommand. Optional modules add a field here and hook into accounts in
// newServices.
type services struct {
	limiter  ratelimit.Limiter
	accounts *auth.Service
	// kickstart:begin module=workspaces
	workspaces *workspaces.Service
	// kickstart:end
}

// newServices builds the services, with sessions in the database.
func newServices(cfg config.Config, d *db.DB, logger *slog.Logger) (*services, error) {
	s := &services{limiter: ratelimit.NewMemory()}
	var err error
	s.accounts, err = auth.NewService(d, auth.DBSessions{DB: d}, s.limiter, logger, auth.Options{
		AppName:           cfg.AppName,
		BaseURL:           cfg.AppURL,
		MinPasswordLength: cfg.PasswordMinLength,
	})
	if err != nil {
		return nil, err
	}
	// kickstart:begin module=workspaces
	s.workspaces, err = workspaces.NewService(d, s.accounts, s.limiter, logger, workspaces.Options{
		AppName: cfg.AppName,
		BaseURL: cfg.AppURL,
	})
	if err != nil {
		return nil, err
	}
	// kickstart:end
	return s, nil
}

// newSender returns the email sender cfg describes: SMTP, or, in dev without
// an SMTP host, the log.
func newSender(cfg config.Config, logger *slog.Logger) (email.Sender, error) {
	if cfg.SMTPHost == "" {
		return email.Log{Logger: logger}, nil
	}
	return email.NewSMTP(email.SMTPConfig{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.SMTPFrom,
		TLS:      cfg.SMTPTLS,
	})
}

// startJobs starts the job worker, with every job handler registered, and
// the scheduler, with every recurring task. When ctx is cancelled they stop
// taking work; the returned channel closes once running jobs have finished.
func startJobs(ctx context.Context, cfg config.Config, d *db.DB, svc *services, logger *slog.Logger) (<-chan struct{}, error) {
	sender, err := newSender(cfg, logger)
	if err != nil {
		return nil, err
	}

	worker := jobs.NewWorker(d, logger)
	worker.Handle(email.JobKind, email.Handler(sender))
	worker.Handle(jobs.CleanupKind, jobs.Cleanup(d, logger))
	worker.Handle(auth.CleanupKind, svc.accounts.Cleanup())
	// kickstart:begin module=workspaces
	worker.Handle(workspaces.CleanupKind, svc.workspaces.Cleanup())
	// kickstart:end

	scheduler := jobs.NewScheduler(d, logger)
	scheduler.Every(24*time.Hour, jobs.CleanupKind)
	scheduler.Every(time.Hour, auth.CleanupKind)
	// kickstart:begin module=workspaces
	scheduler.Every(24*time.Hour, workspaces.CleanupKind)
	// kickstart:end

	var wg sync.WaitGroup
	wg.Go(func() { scheduler.Run(ctx) })
	wg.Go(func() { worker.Run(ctx, cfg.WorkerConcurrency) })
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	logger.Info("job worker started", "concurrency", cfg.WorkerConcurrency)
	return done, nil
}

// waitForJobs waits for the worker to finish its running jobs, or for
// deadline to pass. Jobs still running then are abandoned; another worker
// retries them once their lease runs out.
func waitForJobs(done <-chan struct{}, deadline context.Context, logger *slog.Logger) {
	select {
	case <-done:
		logger.Info("job worker stopped")
	case <-deadline.Done():
		logger.Warn("job worker still busy at shutdown; unfinished jobs will be retried")
	}
}
