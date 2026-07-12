package repository

import (
	"context"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// GroupRepo is the data-access client for groups.
type GroupRepo struct{ db *gorm.DB }

// GetByID loads a group by primary key.
func (r *GroupRepo) GetByID(ctx context.Context, id uint) (*model.Group, error) {
	var g model.Group
	err := r.db.WithContext(ctx).First(&g, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &g, err
}
