package repository

import (
	"context"
	"errors"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// WebDAVAccountRepo is the data-access client for WebDAV credentials.
type WebDAVAccountRepo struct{ db *gorm.DB }

// Create inserts a new WebDAV account.
func (r *WebDAVAccountRepo) Create(ctx context.Context, a *model.WebDAVAccount) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// ListByUser returns a user's WebDAV accounts, oldest first.
func (r *WebDAVAccountRepo) ListByUser(ctx context.Context, userID uint) ([]model.WebDAVAccount, error) {
	var out []model.WebDAVAccount
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("id asc").Find(&out).Error
	return out, err
}

// GetByUsername loads an account (with its hash) by username, for authentication.
func (r *WebDAVAccountRepo) GetByUsername(ctx context.Context, username string) (*model.WebDAVAccount, error) {
	var a model.WebDAVAccount
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &a, err
}

// Delete removes one of a user's accounts (scoped to the owner).
func (r *WebDAVAccountRepo) Delete(ctx context.Context, userID, id uint) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND id = ?", userID, id).
		Delete(&model.WebDAVAccount{}).Error
}

// TouchLastUsed records that an account was just used for a request.
func (r *WebDAVAccountRepo) TouchLastUsed(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.WebDAVAccount{}).
		Where("id = ?", id).
		Update("last_used_at", now).Error
}
