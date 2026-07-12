package model

// Entity types.
const (
	EntityTypeVersion = 0
	EntityTypeThumb   = 1
)

// Entity is a physical stored object. Multiple File rows may share one Entity
// (deduplication) — ReferenceCount tracks how many; the object is removed from
// storage when it reaches zero.
type Entity struct {
	Base
	Type            int    `json:"type"`
	Source          string `gorm:"size:1024;index" json:"source"` // path within the storage backend
	Size            int64  `json:"size"`
	ReferenceCount  int    `json:"reference_count"`
	StoragePolicyID uint   `json:"storage_policy_id"`
	UploadSessionID string `gorm:"size:64;index" json:"-"`
	CreatedByID     uint   `json:"created_by_id"`
	Props           JSON   `gorm:"type:json" json:"-"`
}
