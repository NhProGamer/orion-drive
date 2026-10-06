package model

import "time"

// SiteAsset is an admin-uploaded branding image (favicon, banners), served
// publicly in place of the built-in one.
type SiteAsset struct {
	Name        string    `gorm:"primarykey;size:64" json:"name"`
	ContentType string    `gorm:"size:100" json:"content_type"`
	ETag        string    `gorm:"column:etag;size:64" json:"etag"`
	Data        []byte    `json:"-"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName pins the table name.
func (SiteAsset) TableName() string { return "site_assets" }
