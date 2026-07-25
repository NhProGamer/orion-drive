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
