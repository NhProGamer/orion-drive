package repository

import (
	"context"
	"errors"
	"time"

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

// SetModifiedTime overrides a file's modification time (updated_at), owner-scoped.
// UpdateColumn bypasses GORM's auto-update so the given time is stored verbatim
// (used to honour a client-supplied mtime, e.g. WebDAV X-OC-Mtime).
func (r *FileRepo) SetModifiedTime(ctx context.Context, ownerID, id uint, t time.Time) error {
	return r.db.WithContext(ctx).Model(&model.File{}).
		Where("owner_id = ? AND id = ?", ownerID, id).
		UpdateColumn("updated_at", t).Error
}

// ChildrenOfMany returns the non-trashed direct children of any of parentIDs,
// owner-scoped. Used to walk a subtree one level at a time (e.g. folder size).
func (r *FileRepo) ChildrenOfMany(ctx context.Context, ownerID uint, parentIDs []uint) ([]model.File, error) {
	var files []model.File
	if len(parentIDs) == 0 {
		return files, nil
	}
	err := r.db.WithContext(ctx).
		Select("id", "type", "size", "parent_id").
		Where("owner_id = ? AND parent_id IN ?", ownerID, parentIDs).
		Find(&files).Error
	return files, err
}

// ListChildrenUnscoped is like ListChildren but also returns trashed children,
// used to walk a subtree that is itself in the recycle bin.
func (r *FileRepo) ListChildrenUnscoped(ctx context.Context, ownerID uint, parentID *uint) ([]model.File, error) {
	q := r.db.WithContext(ctx).Unscoped().Where("owner_id = ?", ownerID)
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
	// Only the top-most trashed items are "trash roots" — a trashed item whose
	// parent is also trashed was removed as part of its ancestor and must not
	// clutter the recycle bin (it is restored/purged with the ancestor).
	trashedIDs := r.db.Model(&model.File{}).Unscoped().
		Select("id").
		Where("owner_id = ? AND trashed_at IS NOT NULL", ownerID)
	var files []model.File
	err := r.db.WithContext(ctx).Unscoped().
		Where("owner_id = ? AND trashed_at IS NOT NULL", ownerID).
		Where("parent_id IS NULL OR parent_id NOT IN (?)", trashedIDs).
		Order("type desc, name asc").Find(&files).Error
	return files, err
}

// SearchFilters narrows a search. All fields are optional; a zero value means
// "no constraint". The name query is a case-insensitive substring match.
type SearchFilters struct {
	Query   string // substring of the file name
	Type    string // "", "file" or "folder"
	Starred bool   // only starred items
	MinSize int64  // bytes; 0 = ignore
	MaxSize int64  // bytes; 0 = ignore
	After   *time.Time
	Before  *time.Time
}

// Search returns non-trashed files owned by ownerID matching the filters,
// across every folder (recursive). Category (kind) filtering is applied by the
// caller since it depends on the file extension, not a column.
func (r *FileRepo) Search(ctx context.Context, ownerID uint, f SearchFilters) ([]model.File, error) {
	q := r.db.WithContext(ctx).Where("owner_id = ?", ownerID)
	if f.Query != "" {
		q = q.Where("name LIKE ?", "%"+f.Query+"%")
	}
	switch f.Type {
	case "file":
		q = q.Where("type = ?", model.FileTypeFile)
	case "folder":
		q = q.Where("type = ?", model.FileTypeFolder)
	}
	if f.Starred {
		q = q.Where("starred = ?", true)
	}
	if f.MinSize > 0 {
		q = q.Where("size >= ?", f.MinSize)
	}
	if f.MaxSize > 0 {
		q = q.Where("size <= ?", f.MaxSize)
	}
	if f.After != nil {
		q = q.Where("updated_at >= ?", *f.After)
	}
	if f.Before != nil {
		q = q.Where("updated_at <= ?", *f.Before)
	}
	var files []model.File
	err := q.Order("type desc, name asc").Find(&files).Error
	return files, err
}

// AllFolders returns lightweight rows (id, name, parent) for every non-trashed
// folder owned by ownerID — used to resolve the location path of search hits.
func (r *FileRepo) AllFolders(ctx context.Context, ownerID uint) ([]model.File, error) {
	var files []model.File
	err := r.db.WithContext(ctx).
		Select("id", "name", "parent_id").
		Where("owner_id = ? AND type = ?", ownerID, model.FileTypeFolder).
		Find(&files).Error
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

// ListTrashedBefore returns files (any owner) trashed before the given time —
// used by the background trash-purge job.
func (r *FileRepo) ListTrashedBefore(ctx context.Context, before time.Time) ([]model.File, error) {
	var files []model.File
	err := r.db.WithContext(ctx).Unscoped().
		Where("trashed_at IS NOT NULL AND trashed_at < ?", before).
		Find(&files).Error
	return files, err
}

// CountAll returns the number of (non-trashed) files across all users.
func (r *FileRepo) CountAll(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.File{}).
		Where("type = ?", model.FileTypeFile).Count(&n).Error
	return n, err
}
