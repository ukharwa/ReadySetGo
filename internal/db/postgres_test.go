//go:build postgres

package db_test

import (
	"log/slog"
	"sync"
	"testing"

	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/db/dbtest"
)

// TestConcurrentMigrations starts several migration runs at once against an
// empty database, as instances starting together would. The advisory lock
// must let each migration apply exactly once.
func TestConcurrentMigrations(t *testing.T) {
	t.Parallel()
	url := dbtest.URL(t)
	ctx := t.Context()

	// The test database is a migrated copy; roll it back to empty first.
	d, err := db.Open(ctx, db.Options{URL: url, MaxOpenConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	p, err := d.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			inst, err := db.Open(ctx, db.Options{URL: url, MaxOpenConns: 4})
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = inst.Close() }()
			errs <- inst.MigrateUp(ctx, slog.New(slog.DiscardHandler))
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migrate: %v", err)
		}
	}
	if v, err := p.GetDBVersion(ctx); err != nil || v == 0 {
		t.Errorf("version after concurrent migrations = %d (err %v), want the latest", v, err)
	}
}
