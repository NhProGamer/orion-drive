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

// Create inserts a new group.
func (r *GroupRepo) Create(ctx context.Context, g *model.Group) error {
	return r.db.WithContext(ctx).Create(g).Error
}

// FindBySSOGroups returns the first group (by id) whose SSO mapping matches any
// of the user's SSO groups, or false when none map.
func (r *GroupRepo) FindBySSOGroups(ctx context.Context, ssoGroups []string) (*model.Group, bool) {
	if len(ssoGroups) == 0 {
		return nil, false
	}
	groups, err := r.List(ctx)
	if err != nil {
		return nil, false
	}
	for i := range groups {
		if groups[i].MatchesSSO(ssoGroups) {
			return &groups[i], true
		}
	}
	return nil, false
}

// Update persists changes to a group.
func (r *GroupRepo) Update(ctx context.Context, g *model.Group) error {
	return r.db.WithContext(ctx).Save(g).Error
}

// Delete removes a group by ID.
func (r *GroupRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.Group{}, id).Error
}

// CountUsers returns how many users belong to a group.
func (r *GroupRepo) CountUsers(ctx context.Context, groupID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.User{}).Where("group_id = ?", groupID).Count(&n).Error
	return n, err
}

// SetStoragePolicy points a group at a storage policy.
func (r *GroupRepo) SetStoragePolicy(ctx context.Context, groupID, policyID uint) error {
	return r.db.WithContext(ctx).Model(&model.Group{}).
		Where("id = ?", groupID).
		Update("storage_policy_id", policyID).Error
}

// SetPermissions replaces a group's permission flags.
func (r *GroupRepo) SetPermissions(ctx context.Context, groupID uint, perms model.JSON) error {
	return r.db.WithContext(ctx).Model(&model.Group{}).
		Where("id = ?", groupID).
		Update("permissions", perms).Error
}
