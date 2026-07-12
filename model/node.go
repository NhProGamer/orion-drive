package model

// Node status values.
const (
	NodeStatusActive    = 0
	NodeStatusSuspended = 1
)

// Node types.
const (
	NodeTypeMaster = 0
	NodeTypeSlave  = 1
)

// Node is a member of the OrionDrive cluster. The schema is defined now;
// multi-node orchestration is a later milestone.
type Node struct {
	Base
	Name     string `gorm:"size:255" json:"name"`
	Type     int    `json:"type"`
	Status   int    `json:"status"`
	Server   string `gorm:"size:1024" json:"server"`
	SlaveKey string `gorm:"size:512" json:"-"`
	Weight   int    `json:"weight"`
	Settings JSON   `gorm:"type:json" json:"-"`
}
