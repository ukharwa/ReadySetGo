package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/db/dbtest"
)

// testEnv returns the settings a command runs with in a test: dev mode, a
// database and folders of its own, and the values in more.
func testEnv(t *testing.T, more map[string]string) func(string) string {
	env := map[string]string{
		"APP_ENV":      "dev",
		"DATABASE_URL": dbtest.URL(t),
		// readysetgo:begin module=uploads
		"UPLOADS_DIR": t.TempDir(),
		// readysetgo:end
	}
	maps.Copy(env, more)
	return func(k string) string { return env[k] }
}

func TestRunUnknownCommand(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"nope"}} {
		if err := run(t.Context(), args, func(string) string { return "" }, io.Discard); err == nil {
			t.Errorf("run(%q) succeeded, want error", args)
		}
	}
}

func TestUsageListsCommands(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := run(t.Context(), []string{"-h"}, func(string) string { return "" }, &out); err != nil {
		t.Fatal(err)
	}
	for _, c := range commands() {
		if !strings.Contains(out.String(), c.name) {
			t.Errorf("usage does not list %q:\n%s", c.name, out.String())
		}
	}
}

// TestServe starts the real server against a fresh database, waits until it
// is ready, then cancels its context and checks that it shuts down cleanly.
func TestServe(t *testing.T) {
	t.Parallel()
	addr := freeAddr(t)
	getenv := testEnv(t, map[string]string{"HTTP_ADDR": addr, "LOG_LEVEL": "error"})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"serve"}, getenv, io.Discard) }()

	waitFor(t, "http://"+addr+"/healthz", http.StatusOK)
	waitFor(t, "http://"+addr+"/readyz", http.StatusOK)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not shut down")
	}
}

// TestWorker starts the standalone worker and checks that it stops cleanly
// on cancellation.
func TestWorker(t *testing.T) {
	t.Parallel()
	getenv := testEnv(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"worker"}, getenv, &out) }()

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "job worker started") {
		if time.Now().After(deadline) {
			t.Fatalf("worker didn't start:\n%s", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker returned %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestMigrateStatus(t *testing.T) {
	t.Parallel()
	getenv := testEnv(t, map[string]string{"LOG_LEVEL": "error"})

	if err := run(t.Context(), []string{"migrate", "up"}, getenv, io.Discard); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	var out bytes.Buffer
	if err := run(t.Context(), []string{"migrate", "status"}, getenv, &out); err != nil {
		t.Fatalf("migrate status: %v", err)
	}
	if !strings.Contains(out.String(), "00001_users.sql") || strings.Contains(out.String(), "pending") {
		t.Errorf("status after up should list 00001_users.sql as applied:\n%s", out.String())
	}
}

func TestUserCommand(t *testing.T) {
	t.Parallel()
	getenv := testEnv(t, map[string]string{"LOG_LEVEL": "error"})
	user := func(args ...string) error {
		return run(t.Context(), append([]string{"user"}, args...), getenv, io.Discard)
	}
	if err := run(t.Context(), []string{"seed"}, getenv, io.Discard); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, args := range [][]string{{"promote", "user@example.com"}, {"disable", "user@example.com"}, {"delete", "user@example.com"}} {
		if err := user(args...); err != nil {
			t.Errorf("user %v: %v", args, err)
		}
	}
	if err := user("enable", "user@example.com"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("enabling a deleted user: err = %v, want ErrUserNotFound", err)
	}
	if err := user("rename", "admin@example.com"); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("unknown action: err = %v, want the usage", err)
	}
}

func TestCreateMigration(t *testing.T) {
	t.Parallel()
	dirs := []string{t.TempDir(), t.TempDir()}
	if err := os.WriteFile(filepath.Join(dirs[0], "00007_existing.sql"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := createMigration(dirs, "add_notes", io.Discard); err != nil {
		t.Fatalf("createMigration: %v", err)
	}
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, "00008_add_notes.sql")); err != nil {
			t.Errorf("expected 00008_add_notes.sql in every folder: %v", err)
		}
	}
	if err := createMigration(dirs, "Bad Name", io.Discard); err == nil {
		t.Error("createMigration accepted an invalid name")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// waitFor polls url until it returns status. Keep-alives are off: an unused
// pooled connection would make the server's Shutdown wait up to 5 seconds.
func waitFor(t *testing.T, url string, status int) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == status {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s did not return %d within 10s", url, status)
}

// syncBuffer is a bytes.Buffer safe to write from one goroutine and read
// from another.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
