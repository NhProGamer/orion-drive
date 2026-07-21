package model

import "time"

// APIToken is a personal access token a user creates to authenticate
// non-interactive clients (native apps, the KDE KIO worker, sync clients) via
// the Bearer scheme, instead of the interactive OIDC session. It is a delegated
// credential — minted by an already-SSO-authenticated user — so it does not
// bypass the SSO-only login. Only a SHA-256 hash of the token is stored; the
// plaintext is shown once at creation.
type APIToken struct {
	Base
	UserID     uint       `gorm:"index" json:"user_id"`
	Label      string     `gorm:"size:255" json:"label"`
	Prefix     string     `gorm:"size:16" json:"prefix"` // leading chars, for display
	TokenHash  string     `gorm:"uniqueIndex;size:64" json:"-"`
	ReadOnly   bool       `json:"read_only"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// TableName pins the table name.
func (APIToken) TableName() string { return "api_tokens" }

// Expired reports whether the token's expiry has passed.
func (t *APIToken) Expired() bool { return t.ExpiresAt != nil && time.Now().After(*t.ExpiresAt) }
