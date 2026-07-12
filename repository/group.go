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

// List returns every group, oldest first.
func (r *GroupRepo) List(ctx context.Context) ([]model.Group, error) {
	var groups []model.Group
	err := r.db.WithContext(ctx).Order("id asc").Find(&groups).Error
	return groups, err
}

// SetStoragePolicy points a group at a storage policy.
func (r *GroupRepo) SetStoragePolicy(ctx context.Context, groupID, policyID uint) error {
	return r.db.WithContext(ctx).Model(&model.Group{}).
		Where("id = ?", groupID).
		Update("storage_policy_id", policyID).Error
}
