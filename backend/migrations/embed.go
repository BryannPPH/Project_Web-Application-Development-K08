package migrations

import "embed"

// Files memuat migrasi skema berversi untuk basis data platform.
//
//go:embed *.sql
var Files embed.FS
