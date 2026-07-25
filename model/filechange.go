package model

import "time"

// FileChange is one entry in the append-only change journal that powers WebDAV
// sync-collection (RFC 6578). ID is the monotonic sync token; Deleted marks a
// tombstone (the member was removed).
type FileChange struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"index:idx_file_changes_user_id,priority:1" json:"user_id"`
	Path      string    `gorm:"size:4096" json:"path"`
	IsDir     bool      `json:"is_dir"`
	Deleted   bool      `json:"deleted"`
	CreatedAt time.Time `json:"created_at"`
}
