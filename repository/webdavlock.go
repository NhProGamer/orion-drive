package repository

import (
	"context"
	"errors"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// WebDAVLockRepo is the data-access client for persisted WebDAV locks.
type WebDAVLockRepo struct{ db *gorm.DB }

// Create inserts a lock.
func (r *WebDAVLockRepo) Create(ctx context.Context, l *model.WebDAVLock) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// GetByToken loads a user's lock by its token.
func (r *WebDAVLockRepo) GetByToken(ctx context.Context, userID uint, token string) (*model.WebDAVLock, error) {
	var l model.WebDAVLock
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND token = ?", userID, token).First(&l).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &l, err
}

// ListActive returns a user's non-expired locks.
func (r *WebDAVLockRepo) ListActive(ctx context.Context, userID uint, now time.Time) ([]model.WebDAVLock, error) {
	var out []model.WebDAVLock
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND expires > ?", userID, now).Find(&out).Error
	return out, err
}

// Refresh extends a lock's expiry.
func (r *WebDAVLockRepo) Refresh(ctx context.Context, userID uint, token string, expires time.Time) error {
	return r.db.WithContext(ctx).Model(&model.WebDAVLock{}).
		Where("user_id = ? AND token = ?", userID, token).
		Update("expires", expires).Error
}

// DeleteByToken removes a user's lock.
func (r *WebDAVLockRepo) DeleteByToken(ctx context.Context, userID uint, token string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND token = ?", userID, token).
		Delete(&model.WebDAVLock{}).Error
}

// DeleteExpired purges expired locks (maintenance / opportunistic cleanup).
func (r *WebDAVLockRepo) DeleteExpired(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).
		Where("expires <= ?", now).Delete(&model.WebDAVLock{}).Error
}
