package bootstrap

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/migrations"
	"github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenDatabase opens the configured database. SQLite (pure-Go, no CGO) is the
// default; PostgreSQL and MySQL are also supported. All three drivers are
// CGO-free so the binary stays static.
func OpenDatabase(cfg *conf.Config) (*gorm.DB, error) {
	gormCfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}

	switch cfg.Database.Type {
	case "", "sqlite":
		if dir := filepath.Dir(cfg.Database.DBFile); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create db dir: %w", err)
			}
		}
		// SQLite tuning for a concurrent server: WAL lets readers run while a
		// writer is active, busy_timeout makes a contended lock wait instead of
		// failing immediately, NORMAL sync is safe under WAL. Without these,
		// parallel writers (e.g. several SFTP uploads at once) hit SQLITE_BUSY.
		dsn := cfg.Database.DBFile +
			"?_pragma=journal_mode(WAL)" +
			"&_pragma=busy_timeout(10000)" +
			"&_pragma=synchronous(NORMAL)" +
			"&_pragma=foreign_keys(ON)"
		db, err := gorm.Open(sqlite.Open(dsn), gormCfg)
		if err != nil {
			return nil, err
		}
		// SQLite allows only ONE writer at a time, and the pure-Go driver breaks
		// under concurrent transactions ("cannot start a transaction within a
		// transaction"). Serialise all access through a single connection so
		// concurrent operations queue cleanly instead of erroring.
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.SetMaxOpenConns(1)
		}
		return db, nil
	case "postgres":
		return gorm.Open(postgres.Open(postgresDSN(cfg.Database)), gormCfg)
	case "mysql":
		return gorm.Open(mysql.Open(mysqlDSN(cfg.Database)), gormCfg)
	default:
		return nil, fmt.Errorf("database type %q is not supported (use sqlite, postgres or mysql)", cfg.Database.Type)
	}
}

// postgresDSN builds a key/value PostgreSQL DSN from the configured fields.
func postgresDSN(d conf.Database) string {
	host := firstNonEmpty(d.Host, "localhost")
	port := firstNonEmpty(d.Port, "5432")
	sslmode := firstNonEmpty(d.SSLMode, "disable")
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, d.User, d.Password, firstNonEmpty(d.Name, "orion"), sslmode)
}

// mysqlDSN builds a go-sql-driver/mysql DSN. parseTime is required so DATETIME
// columns scan into time.Time; charset utf8mb4 matches the schema.
func mysqlDSN(d conf.Database) string {
	addr := net.JoinHostPort(firstNonEmpty(d.Host, "localhost"), firstNonEmpty(d.Port, "3306"))
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		d.User, d.Password, addr, firstNonEmpty(d.Name, "orion"))
}

func firstNonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// gooseDialect maps the configured database type to a goose dialect.
func gooseDialect(dbType string) string {
	switch dbType {
	case "postgres":
		return "postgres"
	case "mysql":
		return "mysql"
	default:
		return "sqlite3"
	}
}

// migrationsDir returns the embedded migration subdirectory for the configured
// database, matching the goose dialect.
func migrationsDir(cfg *conf.Config) string {
	return migrations.Dir(gooseDialect(cfg.Database.Type))
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
	return goose.Up(sqlDB, migrationsDir(cfg))
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
	return goose.RunContext(context.Background(), command, sqlDB, migrationsDir(cfg), args...)
}
