// Package filemanager ties the repository and storage drivers together and
// exposes the high-level file operations used by the explorer service.
package filemanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/NhProGamer/orion-drive/repository"
)

// Common errors surfaced to the API layer.
var (
	ErrConflict    = errors.New("a file with that name already exists here")
	ErrInvalidName = errors.New("invalid file name")
	ErrQuota       = errors.New("storage quota exceeded")
	ErrNotFound    = repository.ErrNotFound
	ErrNotAFolder  = errors.New("destination is not a folder")
)

// Manager coordinates logical files (repository) and physical storage (driver).
type Manager struct {
	repo   *repository.Repository
	cache  cache.Store
	tmpDir string
}

// NewManager builds a Manager. tmpDir is where in-progress uploads are staged.
func NewManager(repo *repository.Repository, c cache.Store, tmpDir string) *Manager {
	return &Manager{repo: repo, cache: c, tmpDir: tmpDir}
}

// policyForUser resolves the storage policy that applies to a user.
func (m *Manager) policyForUser(ctx context.Context, user *model.User) (*model.StoragePolicy, error) {
	if user.Group != nil && user.Group.StoragePolicyID != 0 {
		return m.repo.Policy.GetByID(ctx, user.Group.StoragePolicyID)
	}
	return nil, errors.New("user has no storage policy")
}

// driverForPolicy builds the storage Handler for a policy.
func (m *Manager) driverForPolicy(p *model.StoragePolicy) (driver.Handler, error) {
	return driver.New(p)
}

// List returns the children of parentID (nil = drive root) for the user.
func (m *Manager) List(ctx context.Context, user *model.User, parentID *uint) ([]model.File, error) {
	return m.repo.File.ListChildren(ctx, user.ID, parentID)
}

// ListTrashed returns the user's recycle-bin contents.
func (m *Manager) ListTrashed(ctx context.Context, user *model.User) ([]model.File, error) {
	return m.repo.File.ListTrashed(ctx, user.ID)
}

// Search returns non-trashed files matching query.
func (m *Manager) Search(ctx context.Context, user *model.User, query string) ([]model.File, error) {
	return m.repo.File.Search(ctx, user.ID, query)
}

// CreateFolder creates a new folder under parentID.
func (m *Manager) CreateFolder(ctx context.Context, user *model.User, parentID *uint, name string) (*model.File, error) {
	name = strings.TrimSpace(name)
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := m.ensureParent(ctx, user, parentID); err != nil {
		return nil, err
	}
	if _, err := m.repo.File.FindChildByName(ctx, user.ID, parentID, name); err == nil {
		return nil, ErrConflict
	}
	policy, err := m.policyForUser(ctx, user)
	if err != nil {
		return nil, err
	}
	f := &model.File{
		Name:            name,
		Type:            model.FileTypeFolder,
		OwnerID:         user.ID,
		ParentID:        parentID,
		StoragePolicyID: policy.ID,
	}
	if err := m.repo.File.Create(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// Rename changes a file's name.
func (m *Manager) Rename(ctx context.Context, user *model.User, id uint, newName string) (*model.File, error) {
	newName = strings.TrimSpace(newName)
	if err := validateName(newName); err != nil {
		return nil, err
	}
	f, err := m.repo.File.GetByID(ctx, user.ID, id)
	if err != nil {
		return nil, err
	}
	if existing, err := m.repo.File.FindChildByName(ctx, user.ID, f.ParentID, newName); err == nil && existing.ID != id {
		return nil, ErrConflict
	}
	f.Name = newName
	if err := m.repo.File.Update(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// Move relocates files under a new parent (nil = drive root).
func (m *Manager) Move(ctx context.Context, user *model.User, ids []uint, destParentID *uint) error {
	if err := m.ensureParent(ctx, user, destParentID); err != nil {
		return err
	}
	for _, id := range ids {
		f, err := m.repo.File.GetByID(ctx, user.ID, id)
		if err != nil {
			return err
		}
		if destParentID != nil && *destParentID == id {
			return errors.New("cannot move a folder into itself")
		}
		if existing, err := m.repo.File.FindChildByName(ctx, user.ID, destParentID, f.Name); err == nil && existing.ID != id {
			return ErrConflict
		}
		f.ParentID = destParentID
		if err := m.repo.File.Update(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

// ToggleStar flips a file's starred flag.
func (m *Manager) ToggleStar(ctx context.Context, user *model.User, id uint) (*model.File, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, id)
	if err != nil {
		return nil, err
	}
	f.Starred = !f.Starred
	if err := m.repo.File.Update(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// Trash moves files to the recycle bin.
func (m *Manager) Trash(ctx context.Context, user *model.User, ids []uint) error {
	return m.repo.File.Trash(ctx, user.ID, ids)
}

// Restore returns files from the recycle bin.
func (m *Manager) Restore(ctx context.Context, user *model.User, ids []uint) error {
	return m.repo.File.Restore(ctx, user.ID, ids)
}

// Purge permanently deletes files and their physical entities.
func (m *Manager) Purge(ctx context.Context, user *model.User, ids []uint) error {
	for _, id := range ids {
		f, err := m.repo.File.GetByIDUnscoped(ctx, user.ID, id)
		if err != nil {
			continue
		}
		if f.PrimaryEntityID != nil {
			m.removeEntity(ctx, *f.PrimaryEntityID)
			user.StorageUsed -= f.Size
		}
	}
	if user.StorageUsed < 0 {
		user.StorageUsed = 0
	}
	_ = m.repo.User.Update(ctx, user)
	return m.repo.File.Purge(ctx, user.ID, ids)
}

// removeEntity deletes the physical object and its entity row.
func (m *Manager) removeEntity(ctx context.Context, entityID uint) {
	e, err := m.repo.Entity.GetByID(ctx, entityID)
	if err != nil {
		return
	}
	policy, err := m.repo.Policy.GetByID(ctx, e.StoragePolicyID)
	if err == nil {
		if h, err := m.driverForPolicy(policy); err == nil {
			_, _ = h.Delete(ctx, e.Source)
		}
	}
	_ = m.repo.Entity.Delete(ctx, entityID)
}

// Capacity returns used and total bytes for the user's quota.
func (m *Manager) Capacity(ctx context.Context, user *model.User) (used, total int64, err error) {
	used, err = m.repo.File.SumSize(ctx, user.ID)
	if err != nil {
		return 0, 0, err
	}
	if user.Group != nil {
		total = user.Group.MaxStorage
	}
	return used, total, nil
}

// Download opens the primary entity of a file for reading.
func (m *Manager) Download(ctx context.Context, user *model.User, id uint) (driver.ReadSeekCloser, *model.File, error) {
	f, err := m.repo.File.GetByIDUnscoped(ctx, user.ID, id)
	if err != nil {
		return nil, nil, err
	}
	if f.IsFolder() || f.PrimaryEntityID == nil {
		return nil, nil, errors.New("file has no content")
	}
	e, err := m.repo.Entity.GetByID(ctx, *f.PrimaryEntityID)
	if err != nil {
		return nil, nil, err
	}
	policy, err := m.repo.Policy.GetByID(ctx, e.StoragePolicyID)
	if err != nil {
		return nil, nil, err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return nil, nil, err
	}
	rc, err := h.Open(ctx, e.Source)
	if err != nil {
		return nil, nil, err
	}
	return rc, f, nil
}

// ensureParent verifies that parentID (when set) is an existing folder.
func (m *Manager) ensureParent(ctx context.Context, user *model.User, parentID *uint) error {
	if parentID == nil {
		return nil
	}
	p, err := m.repo.File.GetByID(ctx, user.ID, *parentID)
	if err != nil {
		return err
	}
	if !p.IsFolder() {
		return ErrNotAFolder
	}
	return nil
}

func validateName(name string) error {
	if name == "" || name == "." || name == ".." {
		return ErrInvalidName
	}
	if strings.ContainsAny(name, "/\\") {
		return ErrInvalidName
	}
	return nil
}

// newSourcePath builds a unique backend path for a user's new object.
func newSourcePath(userID uint, name string) string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	id := hex.EncodeToString(buf)
	ext := ""
	if i := strings.LastIndex(name, "."); i >= 0 {
		ext = name[i:]
	}
	return fmt.Sprintf("u%d/%s%s", userID, id, ext)
}
