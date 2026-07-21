package repository

import (
	"context"
	"errors"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// APITokenRepo is the data-access client for personal access tokens.
type APITokenRepo struct{ db *gorm.DB }

// Create inserts a token.
func (r *APITokenRepo) Create(ctx context.Context, t *model.APIToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

// ListByUser returns a user's tokens, newest first.
func (r *APITokenRepo) ListByUser(ctx context.Context, userID uint) ([]model.APIToken, error) {
	var out []model.APIToken
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("id desc").Find(&out).Error
	return out, err
}

// GetByHash loads a token by its hash (used to authenticate a Bearer request).
func (r *APITokenRepo) GetByHash(ctx context.Context, hash string) (*model.APIToken, error) {
	var t model.APIToken
	err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &t, err
}

// Delete revokes one of a user's tokens.
func (r *APITokenRepo) Delete(ctx context.Context, userID, id uint) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND id = ?", userID, id).
		Delete(&model.APIToken{}).Error
}

// TouchLastUsed records a token's most recent use.
func (r *APITokenRepo) TouchLastUsed(ctx context.Context, id uint, now time.Time) error {
	return r.db.WithContext(ctx).Model(&model.APIToken{}).
		Where("id = ?", id).Update("last_used_at", now).Error
}

// DeleteExpired purges expired tokens (maintenance).
func (r *APITokenRepo) DeleteExpired(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).
		Where("expires_at IS NOT NULL AND expires_at <= ?", now).
		Delete(&model.APIToken{}).Error
}
