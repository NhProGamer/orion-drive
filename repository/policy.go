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

// List returns every storage policy, oldest first.
func (r *PolicyRepo) List(ctx context.Context) ([]model.StoragePolicy, error) {
	var policies []model.StoragePolicy
	err := r.db.WithContext(ctx).Order("id asc").Find(&policies).Error
	return policies, err
}

// Create inserts a new storage policy.
func (r *PolicyRepo) Create(ctx context.Context, p *model.StoragePolicy) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// Update persists changes to a storage policy.
func (r *PolicyRepo) Update(ctx context.Context, p *model.StoragePolicy) error {
	return r.db.WithContext(ctx).Save(p).Error
}

// Delete removes a storage policy by ID.
func (r *PolicyRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.StoragePolicy{}, id).Error
}

// GroupsUsing returns how many groups reference a storage policy.
func (r *PolicyRepo) GroupsUsing(ctx context.Context, policyID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Group{}).Where("storage_policy_id = ?", policyID).Count(&n).Error
	return n, err
}
