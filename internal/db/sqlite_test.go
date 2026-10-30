//go:build !postgres

package db_test

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/db/dbtest"
	"github.com/ukharwa/kickstart/internal/store"
)

// TestConcurrentWrites checks the single-writer pool: many goroutines writing
// at once must queue rather than fail with SQLITE_BUSY.
func TestConcurrentWrites(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	ctx := t.Context()

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := range 50 {
		wg.Go(func() {
			errs <- d.InTx(ctx, func(tx *sql.Tx) error {
				_, err := store.New(tx).CreateUser(ctx, newUser(fmt.Sprintf("u%d@example.com", i)))
				return err
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}
}

func TestReadPoolIsReadOnly(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	if _, err := store.New(d.Read).CreateUser(t.Context(), newUser("ro@example.com")); err == nil {
		t.Fatal("write through the read pool succeeded, want an error")
	}
}

// TestCloseCheckpoints checks that closing leaves a self-contained database
// file, with the write-ahead log folded back in.
func TestCloseCheckpoints(t *testing.T) {
	t.Parallel()
	path := dbtest.URL(t)
	d, err := db.Open(t.Context(), db.Options{URL: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.MigrateUp(t.Context(), slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.New(d.Write).CreateUser(t.Context(), newUser("wal@example.com")); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path + "-wal"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("write-ahead log still present after Close (stat err = %v)", err)
	}
}

func TestBackup(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	ctx := t.Context()
	user := newUser("backup@example.com")
	if _, err := store.New(d.Write).CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "backup.db")
	if err := d.Backup(ctx, path); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	restored, err := db.Open(ctx, db.Options{URL: path})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restored.Close() }()
	if _, err := store.New(restored.Read).GetUserByID(ctx, user.ID); err != nil {
		t.Errorf("user missing from backup: %v", err)
	}
}
