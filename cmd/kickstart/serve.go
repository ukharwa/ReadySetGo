package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/ukharwa/kickstart/internal/httpx"
	"github.com/ukharwa/kickstart/internal/ratelimit"
)

// serve runs the HTTP server, and the job worker unless WORKER_ENABLED is
// false, until ctx is cancelled. It then shuts down in order: readiness
// fails, the drain period passes, in-flight requests and running jobs get up
// to SHUTDOWN_TIMEOUT to finish, and the database closes.
func serve(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, logger, err := setup(getenv, stdout)
	if err != nil {
		return err
	}
	logger.Info("starting", "config", cfg)

	database, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB(database, logger)
	if err := migrateOnStart(ctx, cfg, database, logger); err != nil {
		return err
	}

	limiter := ratelimit.NewMemory()
	accounts, err := newAccounts(cfg, database, limiter, logger)
	if err != nil {
		return err
	}

	// The worker stops taking jobs on a signal, or if serve fails to start.
	jobsCtx, stopJobs := context.WithCancel(ctx)
	defer stopJobs()
	var jobsDone <-chan struct{}
	if cfg.WorkerEnabled {
		if jobsDone, err = startJobs(jobsCtx, cfg, database, accounts, logger); err != nil {
			return err
		}
	}

	health := &httpx.Health{Check: database.Ping}
	handler, err := routes(cfg, logger, health, accounts, limiter)
	if err != nil {
		return err
	}
	srv := httpx.NewServer(handler, logger)

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	logger.Info("http server listening", "addr", ln.Addr().String())

	select {
	case err := <-serveErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down", "drain", cfg.ShutdownDrain, "timeout", cfg.ShutdownTimeout)
	health.SetShuttingDown()
	time.Sleep(cfg.ShutdownDrain)

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	logger.Info("http server stopped")
	if jobsDone != nil {
		waitForJobs(jobsDone, shutdownCtx, logger)
	}
	return nil
}
