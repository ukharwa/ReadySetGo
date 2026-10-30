//go:build !postgres

package dbtest

import (
	"path/filepath"
	"testing"
)

// URL returns the location of a new, empty database for this test: a file in
// the test's temporary folder. (An in-memory database won't do: the read and
// write pools would each get their own.)
func URL(t testing.TB) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}
