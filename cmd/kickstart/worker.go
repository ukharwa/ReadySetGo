package main

import (
	"context"
	"flag"
	"io"

	// kickstart:begin module=errreport
	"github.com/ukharwa/kickstart/internal/errreport"
	// kickstart:end
)

// worker runs the job worker and scheduler without the HTTP server, so they
// can run as their own process (set WORKER_ENABLED=false for `serve`). On a
// signal it stops taking jobs and waits up to SHUTDOWN_TIMEOUT for running
// ones to finish.
func worker(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, logger, err := setup(getenv, stdout)
	if err != nil {
		return err
	}
	logger.Info("starting worker", "config", cfg)

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
	done, err := startJobs(ctx, cfg, database, svc, logger)
	if err != nil {
		return err
	}
	<-ctx.Done()
	logger.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	deadline, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	waitForJobs(done, deadline, logger)
	// kickstart:begin module=errreport
	errreport.Flush(logger)
	// kickstart:end
	return nil
}
