// Command kickstart is the application binary. Each subcommand is one way of
// running the app; they share configuration and can run as one process or
// as separate processes. Run it with -h to list the subcommands.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
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

// A command is one subcommand. Its dependencies are parameters so tests can
// call it directly.
type command struct {
	name  string
	usage string
	run   func(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error
}

func commands() []command {
	return []command{
		{"serve", "run the HTTP server", serve},
		{"worker", "run the job worker and scheduler without the HTTP server", worker},
		{"migrate", "manage the database schema: up, down, status, or create NAME", migrate},
		{"user", "manage a user: promote, demote, disable or enable EMAIL", user},
		{"seed", "add sample users for development (dev only)", seed},
		// kickstart:begin db=sqlite
		{"backup", "write a snapshot of the database (-dir, -keep)", backup},
		{"restore", "replace the database with a snapshot (-from FILE; stop the app first)", restore},
		// kickstart:end
	}
}

var errUsage = errors.New("expected a command; run with -h for help")

// run dispatches to a subcommand.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	name, args := args[0], args[1:]
	if name == "-h" || name == "-help" || name == "--help" || name == "help" {
		_, err := io.WriteString(stdout, usage())
		return err
	}
	for _, c := range commands() {
		if c.name == name {
			return c.run(ctx, args, getenv, stdout)
		}
	}
	return fmt.Errorf("unknown command %q; run with -h for help", name)
}

func usage() string {
	var b strings.Builder
	b.WriteString("Usage: kickstart <command> [arguments]\n\nCommands:\n")
	for _, c := range commands() {
		fmt.Fprintf(&b, "  %-8s %s\n", c.name, c.usage)
	}
	return b.String()
}
