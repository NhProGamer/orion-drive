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

// Deleting reports whether the policy's deletion has started (Settings key
// "deleting", set by PolicyRepo.MarkDeleting): it takes no new data, and
// Manager.ResumePolicyDeletions finishes the deletion if it was interrupted.
// shortcut: a Settings key instead of a column to skip a migration per dialect; promote to a column if policies grow a status.
func (p *StoragePolicy) Deleting() bool {
	var s struct {
		Deleting bool `json:"deleting"`
	}
	_ = p.Settings.Unmarshal(&s)
	return s.Deleting
}
