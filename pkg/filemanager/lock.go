package filemanager

import (
	"context"

	"github.com/NhProGamer/orion-drive/model"
)

// Lock marks a file as locked by the user, protecting it from modification.
func (m *Manager) Lock(ctx context.Context, user *model.User, id uint) (*model.File, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, id)
	if err != nil {
		return nil, err
	}
	uid := user.ID
	f.LockOwnerID = &uid
	if err := m.repo.File.Update(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// Unlock clears a file's lock.
func (m *Manager) Unlock(ctx context.Context, user *model.User, id uint) (*model.File, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, id)
	if err != nil {
		return nil, err
	}
	f.LockOwnerID = nil
	if err := m.repo.File.Update(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}
