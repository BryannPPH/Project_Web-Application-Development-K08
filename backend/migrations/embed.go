package migrations

import "embed"

// Files contains the versioned schema migrations for the platform database.
//
//go:embed *.sql
var Files embed.FS
