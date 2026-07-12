package model

import "time"

// Share is a public link granting access to a File (or folder) owned by User.
type Share struct {
	Base
	FileID          uint       `gorm:"index" json:"file_id"`
	UserID          uint       `gorm:"index" json:"user_id"`
	Password        string     `gorm:"size:255" json:"-"`
	Views           int        `json:"views"`
	Downloads       int        `json:"downloads"`
	RemainDownloads *int       `json:"remain_downloads"` // nil means unlimited
	Expires         *time.Time `json:"expires"`
	Props           JSON       `gorm:"type:json" json:"-"`
}
