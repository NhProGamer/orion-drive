package model

import "time"

// WebDAVAccount is a dedicated credential a user creates to connect WebDAV
// clients (Finder, Nautilus, rclone, ...). It is independent of the OIDC login:
// WebDAV clients authenticate with the account's username and a generated
// password (stored as a bcrypt hash), never with the user's SSO identity.
type WebDAVAccount struct {
	Base
	UserID       uint       `gorm:"index" json:"user_id"`
	Label        string     `gorm:"size:255" json:"label"`
	Username     string     `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash string     `gorm:"size:255" json:"-"`
	ReadOnly     bool       `json:"read_only"`
	LastUsedAt   *time.Time `json:"last_used_at"`
}

// TableName pins the table name so GORM does not split the "WebDAV" acronym.
func (WebDAVAccount) TableName() string { return "webdav_accounts" }
