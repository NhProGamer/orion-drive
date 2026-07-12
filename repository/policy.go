package repository

import (
	"context"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// PolicyRepo is the data-access client for storage policies.
type PolicyRepo struct{ db *gorm.DB }

// GetByID loads a storage policy by primary key.
func (r *PolicyRepo) GetByID(ctx context.Context, id uint) (*model.StoragePolicy, error) {
	var p model.StoragePolicy
	err := r.db.WithContext(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &p, err
}
