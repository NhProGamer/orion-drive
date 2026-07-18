package model

import "time"

// Share permission levels. read is the default and the only value existing
// shares carry after migration.
const (
	// SharePermRead grants viewing, listing and downloading only.
	SharePermRead = "read"
	// SharePermWrite grants full access: read plus upload, create, rename,
	// move and delete (deletes go to the owner's recycle bin, never purge).
	SharePermWrite = "write"
	// SharePermDeposit is a blind drop box: upload and create folders only, with
	// no listing or downloading of existing content.
	SharePermDeposit = "deposit"
)

// Share is a public link granting access to a File (or folder) owned by User.
type Share struct {
	Base
	Token           string     `gorm:"uniqueIndex;size:64" json:"token"`
	FileID          uint       `gorm:"index" json:"file_id"`
	UserID          uint       `gorm:"index" json:"user_id"`
	Permission      string     `gorm:"size:16;default:read" json:"permission"` // read | write | deposit
	Password        string     `gorm:"size:255" json:"-"`                      // bcrypt hash; empty = no password
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

// perm returns the effective permission, treating an empty value as read.
func (s *Share) Perm() string {
	if s.Permission == "" {
		return SharePermRead
	}
	return s.Permission
}

// CanList reports whether the share exposes folder listings (read/write, not deposit).
func (s *Share) CanList() bool { return s.Perm() == SharePermRead || s.Perm() == SharePermWrite }

// CanDownload reports whether the share allows downloading content.
func (s *Share) CanDownload() bool { return s.CanList() }

// CanUpload reports whether visitors may upload files or create folders.
func (s *Share) CanUpload() bool { return s.Perm() == SharePermWrite || s.Perm() == SharePermDeposit }

// CanModify reports whether visitors may rename, move or overwrite content.
func (s *Share) CanModify() bool { return s.Perm() == SharePermWrite }

// CanDelete reports whether visitors may delete content (to the owner's trash).
func (s *Share) CanDelete() bool { return s.Perm() == SharePermWrite }

// Blind reports whether the share hides existing content (deposit only).
func (s *Share) Blind() bool { return s.Perm() == SharePermDeposit }
