package repository

import (
	"context"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// DirectLinkRepo is the data-access client for direct links.
type DirectLinkRepo struct{ db *gorm.DB }

// Create inserts a new direct link.
func (r *DirectLinkRepo) Create(ctx context.Context, l *model.DirectLink) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// GetByToken loads a direct link by its token.
func (r *DirectLinkRepo) GetByToken(ctx context.Context, token string) (*model.DirectLink, error) {
	var l model.DirectLink
	err := r.db.WithContext(ctx).Where("token = ?", token).First(&l).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &l, err
}

// ListByFile returns a file's direct links owned by ownerID, newest first.
func (r *DirectLinkRepo) ListByFile(ctx context.Context, ownerID, fileID uint) ([]model.DirectLink, error) {
	var ls []model.DirectLink
	err := r.db.WithContext(ctx).
		Where("owner_id = ? AND file_id = ?", ownerID, fileID).
		Order("created_at desc").Find(&ls).Error
	return ls, err
}

// DeleteByToken removes a direct link owned by ownerID.
func (r *DirectLinkRepo) DeleteByToken(ctx context.Context, ownerID uint, token string) error {
	return r.db.WithContext(ctx).
		Where("owner_id = ? AND token = ?", ownerID, token).
		Delete(&model.DirectLink{}).Error
}

// IncrementDownloads bumps a link's download counter.
func (r *DirectLinkRepo) IncrementDownloads(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.DirectLink{}).
		Where("id = ?", id).
		UpdateColumn("downloads", gorm.Expr("downloads + 1")).Error
}
