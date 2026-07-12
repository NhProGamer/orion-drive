package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/migrations"
	"github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenDatabase opens the configured database. Only SQLite (pure-Go, no CGO) is
// wired for Milestone 0; other engines return a clear error.
func OpenDatabase(cfg *conf.Config) (*gorm.DB, error) {
	gormCfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}

	switch cfg.Database.Type {
	case "", "sqlite":
		if dir := filepath.Dir(cfg.Database.DBFile); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create db dir: %w", err)
			}
		}
		return gorm.Open(sqlite.Open(cfg.Database.DBFile), gormCfg)
	default:
		return nil, fmt.Errorf("database type %q is not supported yet", cfg.Database.Type)
	}
}

// gooseDialect maps the configured database type to a goose dialect.
func gooseDialect(dbType string) string {
	switch dbType {
	case "", "sqlite":
		return "sqlite3"
	case "mysql":
		return "mysql"
	case "postgres":
		return "postgres"
	case "mssql":
		return "mssql"
	default:
		return "sqlite3"
	}
}

// setupGoose points goose at the embedded migration files and selects the
// dialect for the configured database.
func setupGoose(cfg *conf.Config) error {
	goose.SetBaseFS(migrations.FS)
	return goose.SetDialect(gooseDialect(cfg.Database.Type))
}

// Migrate applies all pending migrations (schema + seed) silently. Called on
// server startup so the database is always up to date.
func Migrate(db *gorm.DB, cfg *conf.Config) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if err := setupGoose(cfg); err != nil {
		return err
	}
	goose.SetLogger(goose.NopLogger())
	return goose.Up(sqlDB, ".")
}

// RunGoose executes a goose command (up, down, status, version, reset, ...)
// with visible output. Used by the `migrate` CLI command.
func RunGoose(db *gorm.DB, cfg *conf.Config, command string, args ...string) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if err := setupGoose(cfg); err != nil {
		return err
	}
	return goose.RunContext(context.Background(), command, sqlDB, ".", args...)
}
