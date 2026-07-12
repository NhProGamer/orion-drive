// Package migrations embeds the goose SQL migration files so they ship inside
// the single binary.
package migrations

import "embed"

// FS holds every *.sql migration, applied by goose in filename order.
//
//go:embed *.sql
var FS embed.FS
