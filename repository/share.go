package repository

import (
	"context"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// ShareRepo is the data-access client for share links.
type ShareRepo struct{ db *gorm.DB }

// Create inserts a new share.
func (r *ShareRepo) Create(ctx context.Context, s *model.Share) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// GetByToken loads a share by its public token.
func (r *ShareRepo) GetByToken(ctx context.Context, token string) (*model.Share, error) {
	var s model.Share
	err := r.db.WithContext(ctx).Where("token = ?", token).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &s, err
}

// ListByUser returns a user's shares, newest first.
func (r *ShareRepo) ListByUser(ctx context.Context, userID uint) ([]model.Share, error) {
	var shares []model.Share
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("id desc").Find(&shares).Error
	return shares, err
}

// DeleteByToken removes a user's share by token.
func (r *ShareRepo) DeleteByToken(ctx context.Context, userID uint, token string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND token = ?", userID, token).
		Delete(&model.Share{}).Error
}

// IncrementViews bumps the view counter.
func (r *ShareRepo) IncrementViews(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.Share{}).
		Where("id = ?", id).
		UpdateColumn("views", gorm.Expr("views + 1")).Error
}

// RegisterDownload increments the download counter and decrements the remaining
// allowance (when limited) in a single statement.
func (r *ShareRepo) RegisterDownload(ctx context.Context, s *model.Share) error {
	updates := map[string]any{"downloads": gorm.Expr("downloads + 1")}
	if s.RemainDownloads != nil {
		updates["remain_downloads"] = gorm.Expr("remain_downloads - 1")
	}
	return r.db.WithContext(ctx).Model(&model.Share{}).
		Where("id = ?", s.ID).Updates(updates).Error
}
