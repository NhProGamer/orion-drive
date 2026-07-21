// Package repository is the data-access layer over GORM. Each entity gets a
// small typed client; Repository aggregates them.
package repository

import "gorm.io/gorm"

// Repository groups every entity client behind one struct.
type Repository struct {
	DB     *gorm.DB
	User   *UserRepo
	Group  *GroupRepo
	Policy *PolicyRepo
	File       *FileRepo
	Entity     *EntityRepo
	Share      *ShareRepo
	DirectLink *DirectLinkRepo
	WebDAV     *WebDAVAccountRepo
	WebDAVLock *WebDAVLockRepo
	APIToken   *APITokenRepo
}

// New builds a Repository bound to db.
func New(db *gorm.DB) *Repository {
	return &Repository{
		DB:     db,
		User:   &UserRepo{db: db},
		Group:  &GroupRepo{db: db},
		Policy: &PolicyRepo{db: db},
		File:       &FileRepo{db: db},
		Entity:     &EntityRepo{db: db},
		Share:      &ShareRepo{db: db},
		DirectLink: &DirectLinkRepo{db: db},
		WebDAV:     &WebDAVAccountRepo{db: db},
		WebDAVLock: &WebDAVLockRepo{db: db},
		APIToken:   &APITokenRepo{db: db},
	}
}
