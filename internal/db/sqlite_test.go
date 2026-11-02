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

	"github.com/ukharwa/readysetgo/internal/auth/store"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/db/dbtest"
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

func TestRestoreFile(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := dbtest.URL(t)
	d, err := db.Open(ctx, db.Options{URL: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.MigrateUp(ctx, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	before := newUser("before@example.com")
	if _, err := store.New(d.Write).CreateUser(ctx, before); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if err := d.Backup(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	after := newUser("after@example.com")
	if _, err := store.New(d.Write).CreateUser(ctx, after); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	if err := db.RestoreFile(ctx, snapshot, path); err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	d, err = db.Open(ctx, db.Options{URL: path})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	q := store.New(d.Read)
	if _, err := q.GetUserByID(ctx, before.ID); err != nil {
		t.Errorf("user from before the snapshot is missing: %v", err)
	}
	if _, err := q.GetUserByID(ctx, after.ID); err == nil {
		t.Error("user added after the snapshot survived the restore")
	}

	junk := filepath.Join(t.TempDir(), "junk.db")
	if err := os.WriteFile(junk, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.RestoreFile(ctx, junk, path); err == nil {
		t.Error("restored a file that isn't a database")
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
