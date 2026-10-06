package repository

import (
	"context"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SettingRepo is the data-access client for admin-editable key/value settings.
type SettingRepo struct{ db *gorm.DB }

// Get returns the value stored under key, or "" when the key was never set.
func (r *SettingRepo) Get(ctx context.Context, key string) (string, error) {
	var s model.Setting
	// A struct condition lets GORM quote "key" per dialect (reserved in MySQL).
	err := r.db.WithContext(ctx).Where(&model.Setting{Key: key}).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return s.Value, err
}

// Set stores value under key, inserting or overwriting it.
func (r *SettingRepo) Set(ctx context.Context, key, value string) error {
	s := model.Setting{Key: key, Value: value}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&s).Error
}
