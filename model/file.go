package model

import (
	"path"
	"strings"

	"gorm.io/gorm"
)

// File types.
const (
	FileTypeFile   = 0
	FileTypeFolder = 1
)

// File is a logical node in a user's virtual filesystem. A File of type
// FileTypeFile points at one or more physical Entities (its versions), the
// current one being PrimaryEntityID. Trash uses GORM soft-delete: a trashed
// file has a non-null TrashedAt and is excluded from normal queries.
type File struct {
	Base
	Name            string         `gorm:"size:255;index" json:"name"`
	Type            int            `json:"type"`
	OwnerID         uint           `gorm:"index" json:"owner_id"`
	ParentID        *uint          `gorm:"index" json:"parent_id"`
	PrimaryEntityID *uint          `json:"primary_entity_id"`
	Size            int64          `json:"size"`
	Starred         bool           `json:"starred"`
	IsSymbolic      bool           `json:"is_symbolic"`
	StoragePolicyID uint           `json:"storage_policy_id"`
	// LockOwnerID, when set, marks the file as locked by that user.
	LockOwnerID *uint          `json:"lock_owner_id"`
	Props       JSON           `gorm:"type:json" json:"-"`
	TrashedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// IsFolder reports whether the file is a directory.
func (f *File) IsFolder() bool { return f.Type == FileTypeFolder }

// IsLocked reports whether the file is currently locked.
func (f *File) IsLocked() bool { return f.LockOwnerID != nil }

// Ext returns the lower-cased extension (without the dot), or "".
func (f *File) Ext() string {
	e := strings.TrimPrefix(path.Ext(f.Name), ".")
	return strings.ToLower(e)
}
