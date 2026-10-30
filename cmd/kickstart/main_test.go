package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestRunUnknownCommand(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"nope"}} {
		if err := run(t.Context(), args, func(string) string { return "" }, io.Discard); err == nil {
			t.Errorf("run(%q) succeeded, want error", args)
		}
	}
}

// TestServe starts the real server, waits until it answers, then cancels
// its context and checks that it shuts down cleanly.
func TestServe(t *testing.T) {
	t.Parallel()
	addr := freeAddr(t)
	env := map[string]string{"APP_ENV": "dev", "HTTP_ADDR": addr, "LOG_LEVEL": "error"}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"serve"}, func(k string) string { return env[k] }, io.Discard) }()

	waitFor(t, "http://"+addr+"/healthz", http.StatusOK)
	waitFor(t, "http://"+addr+"/readyz", http.StatusOK)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down")
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
	deadline := time.Now().Add(5 * time.Second)
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
	t.Fatalf("%s did not return %d within 5s", url, status)
}
