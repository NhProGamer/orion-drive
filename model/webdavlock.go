package model

import "time"

// WebDAVLock is a persisted WebDAV lock (RFC 4918 Class 2). Locks are stored in
// the database rather than in memory so they survive restarts and are shared
// across cluster nodes. They are scoped per user (each user's WebDAV namespace
// is independent). Depth is 0 (this resource only) or -1 (infinity).
type WebDAVLock struct {
	Base
	Token   string    `gorm:"uniqueIndex;size:80" json:"token"`
	UserID  uint      `gorm:"index" json:"user_id"`
	Path    string    `gorm:"index;size:1024" json:"path"`
	Depth   int       `json:"depth"`
	Owner   string    `gorm:"size:2048" json:"owner"`
	Expires time.Time `gorm:"index" json:"expires"`
}

// TableName pins the table name (avoids GORM splitting the WebDAV acronym).
func (WebDAVLock) TableName() string { return "webdav_locks" }
