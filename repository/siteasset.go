package repository

import (
	"context"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SiteAssetRepo is the data-access client for admin-uploaded branding images.
type SiteAssetRepo struct{ db *gorm.DB }

// Get loads an asset with its data, or ErrNotFound.
func (r *SiteAssetRepo) Get(ctx context.Context, name string) (*model.SiteAsset, error) {
	var a model.SiteAsset
	err := r.db.WithContext(ctx).Where(&model.SiteAsset{Name: name}).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &a, err
}

// GetMeta loads an asset without its image data, or ErrNotFound.
func (r *SiteAssetRepo) GetMeta(ctx context.Context, name string) (*model.SiteAsset, error) {
	var a model.SiteAsset
	err := r.db.WithContext(ctx).Select("name", "content_type", "etag", "updated_at").
		Where(&model.SiteAsset{Name: name}).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &a, err
}

// List returns every asset's metadata, without the image data.
func (r *SiteAssetRepo) List(ctx context.Context) ([]model.SiteAsset, error) {
	var out []model.SiteAsset
	err := r.db.WithContext(ctx).Select("name", "content_type", "etag", "updated_at").Find(&out).Error
	return out, err
}

// Put stores an asset, replacing any previous one under the same name.
func (r *SiteAssetRepo) Put(ctx context.Context, a *model.SiteAsset) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"content_type", "etag", "data", "updated_at"}),
	}).Create(a).Error
}

// Delete removes an asset; deleting a missing one is not an error.
func (r *SiteAssetRepo) Delete(ctx context.Context, name string) error {
	return r.db.WithContext(ctx).Where(&model.SiteAsset{Name: name}).Delete(&model.SiteAsset{}).Error
}
