package model

// DirectLink is a stable, unauthenticated URL for a file's content, intended for
// hotlinking and embedding. Unlike a Share it has no password, expiry or limit.
type DirectLink struct {
	Base
	Token     string `gorm:"uniqueIndex;size:64" json:"token"`
	FileID    uint   `gorm:"index" json:"file_id"`
	OwnerID   uint   `gorm:"index" json:"owner_id"`
	Downloads int    `json:"downloads"`
}
