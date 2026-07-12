package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/glebarez/sqlite"
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

// Migrate creates or updates every table and seeds the default group and
// local storage policy on first run.
func Migrate(db *gorm.DB, cfg *conf.Config) error {
	if err := db.AutoMigrate(model.All()...); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}
	return seed(db, cfg)
}

// seed inserts the baseline records needed for a usable install.
func seed(db *gorm.DB, cfg *conf.Config) error {
	var policyCount int64
	if err := db.Model(&model.StoragePolicy{}).Count(&policyCount).Error; err != nil {
		return err
	}
	if policyCount == 0 {
		policy := model.StoragePolicy{
			Name:     "Default local",
			Type:     model.PolicyTypeLocal,
			BasePath: cfg.Storage.LocalBasePath,
		}
		if err := db.Create(&policy).Error; err != nil {
			return err
		}
		group := model.Group{
			Name:            "Default",
			MaxStorage:      50 << 30, // 50 GiB
			StoragePolicyID: policy.ID,
			Permissions:     model.MustJSON(map[string]bool{"is_admin": true}),
		}
		if err := db.Create(&group).Error; err != nil {
			return err
		}
	}
	return nil
}
