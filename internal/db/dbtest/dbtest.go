// Package dbtest gives each test its own migrated database, so tests run
// against the real database engine and can run in parallel.
package dbtest

import (
	"log/slog"
	"testing"

	"github.com/ukharwa/readysetgo/internal/db"
)

// New returns a migrated database for this test. It is closed when the test
// ends.
func New(t testing.TB) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{
		URL: URL(t),
		// readysetgo:begin db=postgres
		MaxOpenConns: 4,
		// readysetgo:end
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	if err := d.MigrateUp(t.Context(), slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return d
}
