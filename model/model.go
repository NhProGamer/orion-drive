// Package model defines the GORM data models backing OrionDrive.
package model

import "time"

// Base carries the fields shared by every persisted entity.
type Base struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
