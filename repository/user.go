package repository

import (
	"context"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
	"gorm.io/gorm"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

// UserRepo is the data-access client for users.
type UserRepo struct{ db *gorm.DB }

// GetByID loads a user (with its group) by primary key.
func (r *UserRepo) GetByID(ctx context.Context, id uint) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Preload("Group").First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &u, err
}

// GetBySubject loads a user by OIDC subject.
func (r *UserRepo) GetBySubject(ctx context.Context, subject string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Preload("Group").Where("subject = ?", subject).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &u, err
}

// GetByEmail loads a user by email.
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Preload("Group").Where("email = ?", email).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &u, err
}

// List returns every user (with their group), newest first — for the admin panel.
func (r *UserRepo) List(ctx context.Context) ([]model.User, error) {
	var users []model.User
	err := r.db.WithContext(ctx).Preload("Group").Order("id asc").Find(&users).Error
	return users, err
}

// Count returns the number of users.
func (r *UserRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.User{}).Count(&n).Error
	return n, err
}

// Create inserts a new user.
func (r *UserRepo) Create(ctx context.Context, u *model.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

// Update persists changes to an existing user.
func (r *UserRepo) Update(ctx context.Context, u *model.User) error {
	return r.db.WithContext(ctx).Save(u).Error
}

// DefaultGroupID returns the ID of the earliest-created group (used as the
// default for auto-provisioned accounts).
func (r *UserRepo) DefaultGroupID(ctx context.Context) (uint, error) {
	var g model.Group
	if err := r.db.WithContext(ctx).Order("id asc").First(&g).Error; err != nil {
		return 0, err
	}
	return g.ID, nil
}
