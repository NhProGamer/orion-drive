package repository

import (
	"context"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// FileChangeRepo is the data-access client for the WebDAV sync-collection change
// journal.
type FileChangeRepo struct{ db *gorm.DB }

// Append records one change. Best-effort: the journal is a sync optimisation.
func (r *FileChangeRepo) Append(ctx context.Context, c *model.FileChange) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// MaxID returns the user's latest change id (0 when the journal is empty),
// used as the sync token for a fresh (initial) sync.
func (r *FileChangeRepo) MaxID(ctx context.Context, userID uint) (uint, error) {
	var id uint
	err := r.db.WithContext(ctx).Model(&model.FileChange{}).
		Where("user_id = ?", userID).
		Select("COALESCE(MAX(id), 0)").Scan(&id).Error
	return id, err
}

// Since returns the user's changes with id greater than sinceID whose path is
// within the given collection prefix ("/" for the whole drive), ordered by id.
func (r *FileChangeRepo) Since(ctx context.Context, userID, sinceID uint, prefix string) ([]model.FileChange, error) {
	q := r.db.WithContext(ctx).
		Where("user_id = ? AND id > ?", userID, sinceID)
	if prefix != "" && prefix != "/" {
		// Match the collection itself and everything under it.
		q = q.Where("path = ? OR path LIKE ?", prefix, prefix+"/%")
	}
	var out []model.FileChange
	err := q.Order("id asc").Find(&out).Error
	return out, err
}
