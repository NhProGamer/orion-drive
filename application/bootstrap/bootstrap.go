// Package bootstrap builds the dependency container shared across the app.
package bootstrap

import (
	"log/slog"
	"os"

	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/repository"
	"gorm.io/gorm"
)

// Dependency is the application's injected dependency container.
type Dependency struct {
	Config *conf.Config
	Logger *slog.Logger
	DB     *gorm.DB
	Cache  cache.Store
	Repo   *repository.Repository
}

// Init opens the database and assembles the dependency container. It does not
// run migrations (see Migrate) so the `migrate` command can own that step.
func Init(cfg *conf.Config) (*Dependency, error) {
	logger := newLogger(cfg)

	db, err := OpenDatabase(cfg)
	if err != nil {
		return nil, err
	}

	dep := &Dependency{
		Config: cfg,
		Logger: logger,
		DB:     db,
		Cache:  cache.NewMemory(),
		Repo:   repository.New(db),
	}
	return dep, nil
}

func newLogger(cfg *conf.Config) *slog.Logger {
	level := slog.LevelInfo
	if cfg.System.Mode == "debug" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
