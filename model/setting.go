package model

// Setting is a single admin-editable configuration key/value pair.
type Setting struct {
	Key   string `gorm:"primarykey;size:255" json:"key"`
	Value string `gorm:"type:text" json:"value"`
}
