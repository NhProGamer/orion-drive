package repository

import (
	"context"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// FileRepo is the data-access client for logical files and folders.
type FileRepo struct{ db *gorm.DB }

// GetByID loads a non-trashed file owned by ownerID.
func (r *FileRepo) GetByID(ctx context.Context, ownerID, id uint) (*model.File, error) {
	var f model.File
	err := r.db.WithContext(ctx).Where("owner_id = ?", ownerID).First(&f, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &f, err
}

// GetByIDUnscoped loads a file owned by ownerID even if it is trashed (used for
// download and restore/purge).
func (r *FileRepo) GetByIDUnscoped(ctx context.Context, ownerID, id uint) (*model.File, error) {
	var f model.File
	err := r.db.WithContext(ctx).Unscoped().Where("owner_id = ?", ownerID).First(&f, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &f, err
}

// ListChildren returns the non-trashed children of parentID (nil = drive root)
// for the given owner.
func (r *FileRepo) ListChildren(ctx context.Context, ownerID uint, parentID *uint) ([]model.File, error) {
	q := r.db.WithContext(ctx).Where("owner_id = ?", ownerID)
	if parentID == nil {
		q = q.Where("parent_id IS NULL")
	} else {
		q = q.Where("parent_id = ?", *parentID)
	}
	var files []model.File
	err := q.Order("type desc, name asc").Find(&files).Error
	return files, err
}

// ListAllFiles returns every non-trashed file (not folders) owned by ownerID.
// Used by the storage-usage view.
func (r *FileRepo) ListAllFiles(ctx context.Context, ownerID uint) ([]model.File, error) {
	var files []model.File
	err := r.db.WithContext(ctx).
		Where("owner_id = ? AND type = ?", ownerID, model.FileTypeFile).
		Order("size desc").Find(&files).Error
	return files, err
}

// ListTrashed returns every trashed file owned by ownerID.
func (r *FileRepo) ListTrashed(ctx context.Context, ownerID uint) ([]model.File, error) {
	var files []model.File
	err := r.db.WithContext(ctx).Unscoped().
		Where("owner_id = ? AND trashed_at IS NOT NULL", ownerID).
		Order("type desc, name asc").Find(&files).Error
	return files, err
}

// Search returns non-trashed files whose name matches the query for the owner.
func (r *FileRepo) Search(ctx context.Context, ownerID uint, query string) ([]model.File, error) {
	var files []model.File
	err := r.db.WithContext(ctx).
		Where("owner_id = ? AND name LIKE ?", ownerID, "%"+query+"%").
		Order("type desc, name asc").Find(&files).Error
	return files, err
}

// FindChildByName looks up a child by exact name within a parent (nil = root).
func (r *FileRepo) FindChildByName(ctx context.Context, ownerID uint, parentID *uint, name string) (*model.File, error) {
	q := r.db.WithContext(ctx).Where("owner_id = ? AND name = ?", ownerID, name)
	if parentID == nil {
		q = q.Where("parent_id IS NULL")
	} else {
		q = q.Where("parent_id = ?", *parentID)
	}
	var f model.File
	err := q.First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &f, err
}

// Create inserts a new file or folder.
func (r *FileRepo) Create(ctx context.Context, f *model.File) error {
	return r.db.WithContext(ctx).Create(f).Error
}

// Update persists changes (rename, move, star, primary entity, size).
func (r *FileRepo) Update(ctx context.Context, f *model.File) error {
	return r.db.WithContext(ctx).Save(f).Error
}

// Trash soft-deletes the given files (moves them to the recycle bin).
func (r *FileRepo) Trash(ctx context.Context, ownerID uint, ids []uint) error {
	return r.db.WithContext(ctx).
		Where("owner_id = ? AND id IN ?", ownerID, ids).
		Delete(&model.File{}).Error
}

// Restore clears the trashed flag on the given files.
func (r *FileRepo) Restore(ctx context.Context, ownerID uint, ids []uint) error {
	return r.db.WithContext(ctx).Unscoped().Model(&model.File{}).
		Where("owner_id = ? AND id IN ?", ownerID, ids).
		Update("trashed_at", nil).Error
}

// Purge permanently deletes the given files.
func (r *FileRepo) Purge(ctx context.Context, ownerID uint, ids []uint) error {
	return r.db.WithContext(ctx).Unscoped().
		Where("owner_id = ? AND id IN ?", ownerID, ids).
		Delete(&model.File{}).Error
}

// SumSize returns the total size of the owner's non-trashed files.
func (r *FileRepo) SumSize(ctx context.Context, ownerID uint) (int64, error) {
	var total *int64
	err := r.db.WithContext(ctx).Model(&model.File{}).
		Where("owner_id = ? AND type = ?", ownerID, model.FileTypeFile).
		Select("COALESCE(SUM(size), 0)").Scan(&total).Error
	if total == nil {
		return 0, err
	}
	return *total, err
}

// CountAll returns the number of (non-trashed) files across all users.
func (r *FileRepo) CountAll(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.File{}).
		Where("type = ?", model.FileTypeFile).Count(&n).Error
	return n, err
}
