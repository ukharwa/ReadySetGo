// Package migrations embeds the database migrations into the binary. Files
// use goose's SQL format and sequential numbering; create new ones with
// `make migration NAME=…`.
package migrations

import "embed"

// FS holds the migration files.
//
//go:embed *.sql
var FS embed.FS
