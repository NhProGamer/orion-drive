package filemanager

import (
	"context"
	"errors"

	"github.com/NhProGamer/orion-drive/model"
)

// ErrVersionNotFound is returned when a version does not belong to the file.
var ErrVersionNotFound = errors.New("version not found")

// Version describes one stored revision of a file.
type Version struct {
	Entity  *model.Entity
	Current bool
}

// ListVersions returns a file's version history, newest first.
func (m *Manager) ListVersions(ctx context.Context, user *model.User, fileID uint) (*model.File, []Version, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, fileID)
	if err != nil {
		return nil, nil, err
	}
	if f.IsFolder() {
		return nil, nil, errors.New("folders have no versions")
	}
	entities, err := m.repo.Entity.ListVersions(ctx, f.ID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]Version, 0, len(entities))
	for i := range entities {
		e := &entities[i]
		out = append(out, Version{Entity: e, Current: f.PrimaryEntityID != nil && *f.PrimaryEntityID == e.ID})
	}
	return f, out, nil
}

// RestoreVersion makes an older version the current one.
func (m *Manager) RestoreVersion(ctx context.Context, user *model.User, fileID, entityID uint) (*model.File, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, fileID)
	if err != nil {
		return nil, err
	}
	if f.IsLocked() {
		return nil, ErrLocked
	}
	e, err := m.versionOf(ctx, f, entityID)
	if err != nil {
		return nil, err
	}
	f.PrimaryEntityID = &e.ID
	f.Size = e.Size
	if err := m.repo.File.Update(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// DeleteVersion removes a non-current version and its stored object.
func (m *Manager) DeleteVersion(ctx context.Context, user *model.User, fileID, entityID uint) error {
	f, err := m.repo.File.GetByID(ctx, user.ID, fileID)
	if err != nil {
		return err
	}
	if f.IsLocked() {
		return ErrLocked
	}
	e, err := m.versionOf(ctx, f, entityID)
	if err != nil {
		return err
	}
	if f.PrimaryEntityID != nil && *f.PrimaryEntityID == e.ID {
		return errors.New("cannot delete the current version")
	}
	m.removeEntity(ctx, e.ID)
	user.StorageUsed -= e.Size
	if user.StorageUsed < 0 {
		user.StorageUsed = 0
	}
	m.persistStorage(ctx, user)
	return nil
}

// versionOf loads an entity and checks it is a version of the given file.
func (m *Manager) versionOf(ctx context.Context, f *model.File, entityID uint) (*model.Entity, error) {
	e, err := m.repo.Entity.GetByID(ctx, entityID)
	if err != nil {
		return nil, ErrVersionNotFound
	}
	if e.FileID == nil || *e.FileID != f.ID {
		return nil, ErrVersionNotFound
	}
	return e, nil
}
