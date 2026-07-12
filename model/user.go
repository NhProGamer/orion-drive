package model

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
	Settings    JSON   `gorm:"type:json" json:"-"`
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
