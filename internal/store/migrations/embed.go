package migrations

import "embed"

// Files contains sequential SQL migration files for the local SQLite store.
//
//go:embed *.sql
var Files embed.FS
