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
	// FileID links a version entity to its file, giving the file a version history.
	FileID *uint `gorm:"index" json:"file_id"`
	Props  JSON  `gorm:"type:json" json:"-"`
}

// EntityProps is the typed content of Entity.Props.
type EntityProps struct {
	// IV is the base64 AES-CTR initialisation vector when the object is
	// encrypted at rest; empty means the object is stored in the clear.
	IV string `json:"iv,omitempty"`
}

// DecodeProps unmarshals the entity's Props into a typed struct.
func (e *Entity) DecodeProps() EntityProps {
	var p EntityProps
	_ = e.Props.Unmarshal(&p)
	return p
}

// Encrypted reports whether the stored object is encrypted at rest.
func (e *Entity) Encrypted() bool { return e.DecodeProps().IV != "" }
