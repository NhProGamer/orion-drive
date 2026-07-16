// Package migrations embeds the goose SQL migration files so they ship inside
// the single binary. Each supported database engine has its own subdirectory
// (sqlite, postgres, mysql); goose is pointed at the one matching the configured
// dialect and tracks applied versions per database.
package migrations

import "embed"

// FS holds every *.sql migration for every dialect, applied by goose in
// filename order within the selected subdirectory.
//
//go:embed sqlite/*.sql postgres/*.sql mysql/*.sql
var FS embed.FS

// Dir returns the embedded migration subdirectory for a goose dialect
// ("sqlite3", "postgres", "mysql"), defaulting to sqlite.
func Dir(dialect string) string {
	switch dialect {
	case "postgres":
		return "postgres"
	case "mysql":
		return "mysql"
	default:
		return "sqlite"
	}
}
