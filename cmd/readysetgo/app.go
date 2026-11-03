package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/config"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/email"
	"github.com/ukharwa/readysetgo/internal/jobs"
	"github.com/ukharwa/readysetgo/internal/logging"
	"github.com/ukharwa/readysetgo/internal/ratelimit"

	// readysetgo:begin module=admin
	"github.com/ukharwa/readysetgo/internal/admin"
	// readysetgo:end
	// readysetgo:begin module=audit
	"github.com/ukharwa/readysetgo/internal/audit"
	// readysetgo:end
	// readysetgo:begin module=errreport
	"github.com/ukharwa/readysetgo/internal/errreport"
	// readysetgo:end
	// readysetgo:begin module=metrics
	"github.com/ukharwa/readysetgo/internal/metrics"
	// readysetgo:end
	// readysetgo:begin module=uploads
	"github.com/ukharwa/readysetgo/internal/uploads"
	// readysetgo:end
	// readysetgo:begin module=twofactor
	"github.com/ukharwa/readysetgo/internal/twofactor"
	// readysetgo:end
	// readysetgo:begin module=workspaces
	"github.com/ukharwa/readysetgo/internal/workspaces"
	// readysetgo:end

	// example:begin
	"github.com/ukharwa/readysetgo/internal/notes"
	// example:end
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
	// readysetgo:begin module=errreport
	if cfg.SentryDSN != "" {
		h, err := errreport.NewHandler(logger.Handler(), cfg.SentryDSN, cfg.Env)
		if err != nil {
			return config.Config{}, nil, fmt.Errorf("error reporting: %w", err)
		}
		logger = slog.New(h)
	}
	// readysetgo:end
	slog.SetDefault(logger)
	return cfg, logger, nil
}

// openDB opens the database described by cfg.
func openDB(ctx context.Context, cfg config.Config) (*db.DB, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d, err := db.Open(ctx, db.Options{
		URL: cfg.DatabaseURL,
		// readysetgo:begin db=postgres
		ReadURL:         cfg.DatabaseReadURL,
		MaxOpenConns:    cfg.DBMaxOpenConns,
		ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
		// readysetgo:end
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
	// example:begin
	notes *notes.Service
	// example:end
	// readysetgo:begin module=workspaces
	workspaces *workspaces.Service
	// readysetgo:end
	// readysetgo:begin module=audit
	audit *audit.Service
	// readysetgo:end
	// readysetgo:begin module=admin
	admin *admin.Service
	// readysetgo:end
	// readysetgo:begin module=uploads
	uploads *uploads.Service
	// readysetgo:end
	// readysetgo:begin module=metrics
	metrics *metrics.Metrics
	// readysetgo:end
	// readysetgo:begin module=twofactor
	twoFactor *twofactor.Service
	// readysetgo:end
}

// newServices builds the services, with sessions in the database.
func newServices(cfg config.Config, d *db.DB, logger *slog.Logger) (*services, error) {
	s := &services{limiter: ratelimit.NewMemory()}
	var err error
	s.accounts, err = auth.NewService(d, auth.DBSessions{DB: d}, s.limiter, logger, auth.Options{
		AppName:           cfg.AppName,
		BaseURL:           cfg.AppURL,
		MinPasswordLength: cfg.PasswordMinLength,
		InviteOnly:        cfg.Registration == "invite",
	})
	if err != nil {
		return nil, err
	}
	// example:begin
	s.notes = notes.NewService(d, logger)
	// example:end
	// readysetgo:begin module=workspaces
	s.workspaces, err = workspaces.NewService(d, s.accounts, s.limiter, logger, workspaces.Options{
		AppName: cfg.AppName,
		BaseURL: cfg.AppURL,
	})
	if err != nil {
		return nil, err
	}
	// readysetgo:end
	// readysetgo:begin module=audit
	s.audit = audit.NewService(d, s.accounts, logger)
	// readysetgo:end
	// readysetgo:begin module=admin
	s.admin = admin.NewService(d, s.accounts, logger)
	// readysetgo:end
	// readysetgo:begin module=uploads
	files, err := uploads.NewDisk(cfg.UploadsDir)
	if err != nil {
		return nil, err
	}
	s.uploads = uploads.NewService(d, files, s.accounts, logger)
	// readysetgo:end
	// readysetgo:begin module=metrics
	s.metrics = metrics.New(d, logger)
	// readysetgo:end
	// readysetgo:begin module=twofactor
	s.twoFactor, err = twofactor.NewService(d, s.accounts, logger, twofactor.Options{
		AppName: cfg.AppName,
		Key:     cfg.TwoFactorKey,
	})
	if err != nil {
		return nil, err
	}
	// readysetgo:end
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
	// readysetgo:begin module=workspaces
	worker.Handle(workspaces.CleanupKind, svc.workspaces.Cleanup())
	// readysetgo:end
	// readysetgo:begin module=uploads
	worker.Handle(uploads.DeleteFilesKind, svc.uploads.DeleteFiles())
	// readysetgo:end

	scheduler := jobs.NewScheduler(d, logger)
	scheduler.Every(24*time.Hour, jobs.CleanupKind)
	scheduler.Every(time.Hour, auth.CleanupKind)
	// readysetgo:begin module=workspaces
	scheduler.Every(24*time.Hour, workspaces.CleanupKind)
	// readysetgo:end

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
