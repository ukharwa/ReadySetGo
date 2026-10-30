package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/ukharwa/kickstart/internal/config"
	"github.com/ukharwa/kickstart/internal/httpx"
	"github.com/ukharwa/kickstart/internal/logging"
)

// serve runs the HTTP server until ctx is cancelled, then shuts down in
// order: readiness fails, the drain period passes, and in-flight requests
// get up to SHUTDOWN_TIMEOUT to finish.
func serve(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	logger := logging.New(stdout, cfg.LogFormat, cfg.LogLevel)
	slog.SetDefault(logger)
	logger.Info("starting", "config", cfg)

	health := &httpx.Health{}
	srv := httpx.NewServer(routes(health), logger)

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
	logger.Info("stopped")
	return nil
}
