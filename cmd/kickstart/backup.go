//go:build !postgres

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// backup writes a consistent, timestamped snapshot of the SQLite database
// into -dir and deletes all but the newest -keep snapshots. Schedule it with
// cron; Litestream covers continuous off-server copies.
func backup(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dir := fs.String("dir", "backups", "folder to write snapshots to")
	keep := fs.Int("keep", 7, "number of snapshots to keep")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keep < 1 {
		return errors.New("-keep must be at least 1")
	}

	cfg, logger, err := setup(getenv, stdout)
	if err != nil {
		return err
	}
	database, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB(database, logger)

	if err := os.MkdirAll(*dir, 0o750); err != nil {
		return fmt.Errorf("create backup folder: %w", err)
	}
	prefix := strings.TrimSuffix(filepath.Base(cfg.DatabaseURL), filepath.Ext(cfg.DatabaseURL)) + "-"
	path := filepath.Join(*dir, prefix+time.Now().UTC().Format("20060102T150405Z")+".db")

	// Write to a temporary name and rename, so a half-written file never
	// looks like a finished snapshot.
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale %s: %w", tmp, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err := database.Backup(ctx, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("finish backup: %w", err)
	}
	logger.Info("backup written", "path", path)

	return pruneBackups(*dir, prefix, *keep, logger)
}

// pruneBackups deletes all but the newest keep snapshots. Names sort by time
// because the timestamp is fixed-width UTC.
func pruneBackups(dir, prefix string, keep int, logger *slog.Logger) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".db") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for _, name := range names[:max(0, len(names)-keep)] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("delete old backup: %w", err)
		}
		logger.Info("old backup deleted", "file", name)
	}
	return nil
}
