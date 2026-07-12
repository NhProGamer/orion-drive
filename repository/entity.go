package repository

import (
	"context"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// EntityRepo is the data-access client for physical entities.
type EntityRepo struct{ db *gorm.DB }

// GetByID loads an entity by primary key.
func (r *EntityRepo) GetByID(ctx context.Context, id uint) (*model.Entity, error) {
	var e model.Entity
	err := r.db.WithContext(ctx).First(&e, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &e, err
}

// Create inserts a new entity.
func (r *EntityRepo) Create(ctx context.Context, e *model.Entity) error {
	return r.db.WithContext(ctx).Create(e).Error
}

// Delete removes an entity row by ID.
func (r *EntityRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.Entity{}, id).Error
}
