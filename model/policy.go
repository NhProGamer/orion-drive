package model

// Storage policy types.
const (
	PolicyTypeLocal  = "local"
	PolicyTypeS3     = "s3"
	PolicyTypeRemote = "remote"
)

// StoragePolicy describes where and how a group's files are physically stored.
type StoragePolicy struct {
	Base
	Name       string `gorm:"size:255" json:"name"`
	Type       string `gorm:"size:32" json:"type"`
	Server     string `gorm:"size:1024" json:"server"`
	BucketName string `gorm:"size:255" json:"bucket_name"`
	BasePath   string `gorm:"size:1024" json:"base_path"`
	AccessKey  string `gorm:"size:512" json:"-"`
	SecretKey  string `gorm:"size:512" json:"-"`
	NodeID     uint   `json:"node_id"`
	Settings   JSON   `gorm:"type:json" json:"settings"`
}
