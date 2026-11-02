//go:build !postgres

package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ukharwa/readysetgo/internal/db/dbtest"
)

func TestBackupCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env := map[string]string{"APP_ENV": "dev", "LOG_LEVEL": "error", "DATABASE_URL": dbtest.URL(t)}
	if err := run(t.Context(), []string{"backup", "-dir", dir}, func(k string) string { return env[k] }, io.Discard); err != nil {
		t.Fatalf("backup: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "test-*.db"))
	if len(matches) != 1 {
		t.Errorf("backup folder holds %v, want one snapshot", matches)
	}
}

func TestPruneBackups(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{
		"app-20260101T000000Z.db", "app-20260102T000000Z.db", "app-20260103T000000Z.db",
		"other.db", // not a snapshot: must survive
	} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := pruneBackups(dir, "app-", 2, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := []string{"app-20260102T000000Z.db", "app-20260103T000000Z.db", "other.db"}
	if !slices.Equal(got, want) {
		t.Errorf("after prune: %v, want %v", got, want)
	}
}
