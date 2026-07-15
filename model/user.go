package model

import "strings"

// User status values.
const (
	UserStatusActive = 0
	UserStatusBanned = 1
)

// User is an authenticated account. OrionDrive authenticates exclusively via
// OIDC, so there is no password field — Subject is the OIDC "sub" claim.
type User struct {
	Base
	Email       string `gorm:"uniqueIndex;size:320" json:"email"`
	Subject     string `gorm:"index;size:255" json:"-"` // OIDC subject
	Nick        string `gorm:"size:255" json:"nick"`
	Avatar      string `gorm:"size:1024" json:"avatar"`
	Status      int    `json:"status"`
	StorageUsed int64  `json:"storage_used"`
	GroupID     uint   `json:"group_id"`
	Group       *Group `json:"group,omitempty"`
	// SSOGroups is the comma-separated list of the user's SSO groups/roles from
	// their last login (used to resolve admin access and group mapping).
	SSOGroups string `gorm:"size:1024" json:"-"`
	Settings  JSON   `gorm:"type:json" json:"-"`
}

// SSOGroupList returns the user's SSO groups, trimmed and non-empty.
func (u *User) SSOGroupList() []string {
	var out []string
	for _, s := range strings.Split(u.SSOGroups, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// DisplayName returns the nickname, falling back to the email local-part.
func (u *User) DisplayName() string {
	if u.Nick != "" {
		return u.Nick
	}
	for i, c := range u.Email {
		if c == '@' {
			return u.Email[:i]
		}
	}
	return u.Email
}
