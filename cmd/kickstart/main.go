// Command kickstart is the application binary. Each subcommand is one way of
// running the app; they share configuration and can run as one process or
// as separate processes.
//
// Usage:
//
//	kickstart <command>
//
// Commands:
//
//	serve   run the HTTP server
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first signal, restore the default behaviour so a second
	// Ctrl-C kills the process instead of waiting for graceful shutdown.
	context.AfterFunc(ctx, stop)

	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

const usage = `Usage: kickstart <command>

Commands:
  serve   run the HTTP server
`

var errUsage = errors.New("expected a command; run with -h for help")

// run dispatches to a subcommand. Its dependencies are parameters so tests
// can call it directly.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "serve":
		return serve(ctx, args, getenv, stdout)
	case "-h", "-help", "--help", "help":
		_, err := io.WriteString(stdout, usage)
		return err
	default:
		return fmt.Errorf("unknown command %q; run with -h for help", cmd)
	}
}
