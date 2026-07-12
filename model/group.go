package model

// Group is a permission and quota tier shared by many users.
type Group struct {
	Base
	Name            string `gorm:"size:255" json:"name"`
	MaxStorage      int64  `json:"max_storage"` // bytes; 0 means unlimited
	SpeedLimit      int64  `json:"speed_limit"` // bytes/s for downloads; 0 means unlimited
	Permissions     JSON   `gorm:"type:json" json:"permissions"`
	StoragePolicyID uint   `json:"storage_policy_id"`
	Settings        JSON   `gorm:"type:json" json:"-"`
}
