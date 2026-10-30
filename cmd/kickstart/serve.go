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
)

// serve runs the HTTP server until ctx is cancelled, then shuts down in
// order: readiness fails, the drain period passes, in-flight requests get up
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

	if cfg.MigrateOnStart {
		migrateCtx, cancel := context.WithTimeout(ctx, migrateTimeout)
		err := database.MigrateUp(migrateCtx, logger)
		cancel()
		if err != nil {
			return err
		}
	}

	health := &httpx.Health{Check: database.Ping}
	srv := httpx.NewServer(routes(cfg, logger, health), logger)

	ln, err := net.Listen("tcp", cfg.HTTPAddr)
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
	return nil
}
