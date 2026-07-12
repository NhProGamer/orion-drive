package model

import "time"

// Share is a public link granting access to a File (or folder) owned by User.
type Share struct {
	Base
	Token           string     `gorm:"uniqueIndex;size:64" json:"token"`
	FileID          uint       `gorm:"index" json:"file_id"`
	UserID          uint       `gorm:"index" json:"user_id"`
	Password        string     `gorm:"size:255" json:"-"` // bcrypt hash; empty = no password
	Views           int        `json:"views"`
	Downloads       int        `json:"downloads"`
	RemainDownloads *int       `json:"remain_downloads"` // nil means unlimited
	Expires         *time.Time `json:"expires"`
	Props           JSON       `gorm:"type:json" json:"-"`
}

// HasPassword reports whether the share is password-protected.
func (s *Share) HasPassword() bool { return s.Password != "" }

// Expired reports whether the share's expiry has passed.
func (s *Share) Expired() bool { return s.Expires != nil && time.Now().After(*s.Expires) }

// Exhausted reports whether the download limit has been reached.
func (s *Share) Exhausted() bool { return s.RemainDownloads != nil && *s.RemainDownloads <= 0 }
