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

// HashesByIDs returns id → content hash for the given entity ids (skipping
// entities without a stored hash). Used to build WebDAV ETags for a listing in
// one query.
func (r *EntityRepo) HashesByIDs(ctx context.Context, ids []uint) (map[uint]string, error) {
	out := make(map[uint]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID   uint
		Hash string
	}
	if err := r.db.WithContext(ctx).Model(&model.Entity{}).
		Select("id", "hash").Where("id IN ? AND hash <> ''", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row.Hash
	}
	return out, nil
}

// FindDedupSource returns the storage Source of an existing object with the same
// content hash on the same policy, for content-addressed deduplication. Matching
// the policy also matches the encryption state (encryption is per-policy), so
// callers only offer unencrypted policies here. scopeUserID limits the match to
// one creator (per-user dedup); nil matches any user (global). Returns "" when
// no reusable object exists.
func (r *EntityRepo) FindDedupSource(ctx context.Context, hash string, policyID uint, scopeUserID *uint) (string, error) {
	if hash == "" {
		return "", nil
	}
	q := r.db.WithContext(ctx).Model(&model.Entity{}).
		Where("hash = ? AND storage_policy_id = ?", hash, policyID)
	if scopeUserID != nil {
		q = q.Where("created_by_id = ?", *scopeUserID)
	}
	var e model.Entity
	if err := q.Select("source").Order("id asc").First(&e).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return e.Source, nil
}

// CountBySource counts entities (other than excludeID) still referencing a
// storage Source. Used before deleting a physical object so a blob shared via
// deduplication is only removed once the last reference is gone.
func (r *EntityRepo) CountBySource(ctx context.Context, source string, excludeID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Entity{}).
		Where("source = ? AND id <> ?", source, excludeID).Count(&n).Error
	return n, err
}

// Create inserts a new entity.
func (r *EntityRepo) Create(ctx context.Context, e *model.Entity) error {
	return r.db.WithContext(ctx).Create(e).Error
}

// Update persists changes to an existing entity.
func (r *EntityRepo) Update(ctx context.Context, e *model.Entity) error {
	return r.db.WithContext(ctx).Save(e).Error
}

// GetThumb returns a file's thumbnail entity, if any.
func (r *EntityRepo) GetThumb(ctx context.Context, fileID uint) (*model.Entity, error) {
	var e model.Entity
	err := r.db.WithContext(ctx).
		Where("file_id = ? AND type = ?", fileID, model.EntityTypeThumb).
		First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &e, err
}

// ListVersions returns a file's version entities, newest first.
func (r *EntityRepo) ListVersions(ctx context.Context, fileID uint) ([]model.Entity, error) {
	var es []model.Entity
	err := r.db.WithContext(ctx).
		Where("file_id = ? AND type = ?", fileID, model.EntityTypeVersion).
		Order("created_at desc").Find(&es).Error
	return es, err
}

// Delete removes an entity row by ID.
func (r *EntityRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.Entity{}, id).Error
}
