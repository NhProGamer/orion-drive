package filemanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/repository"
)

// PurgeExpiredTrash permanently deletes files that have been in the trash longer
// than retention, freeing their storage and adjusting each owner's usage. It
// returns the number of files purged. A retention of 0 disables the job.
func (m *Manager) PurgeExpiredTrash(ctx context.Context, retention time.Duration) (int, error) {
	if retention <= 0 {
		return 0, nil
	}
	before := time.Now().Add(-retention)
	files, err := m.repo.File.ListTrashedBefore(ctx, before)
	if err != nil || len(files) == 0 {
		return 0, err
	}

	byOwner := map[uint][]uint{}
	freed := map[uint]int64{}
	for i := range files {
		f := &files[i]
		byOwner[f.OwnerID] = append(byOwner[f.OwnerID], f.ID)
		// Drop share links / direct links / thumbnail for the file.
		_ = m.repo.Share.DeleteByFile(ctx, f.OwnerID, f.ID)
		_ = m.repo.DirectLink.DeleteByFile(ctx, f.OwnerID, f.ID)
		if t, err := m.repo.Entity.GetThumb(ctx, f.ID); err == nil {
			m.removeEntity(ctx, t.ID)
		}
		// Remove every stored version and account for the freed bytes.
		versions, _ := m.repo.Entity.ListVersions(ctx, f.ID)
		if len(versions) == 0 && f.PrimaryEntityID != nil {
			m.removeEntity(ctx, *f.PrimaryEntityID)
			freed[f.OwnerID] += f.Size
			continue
		}
		for j := range versions {
			m.removeEntity(ctx, versions[j].ID)
			freed[f.OwnerID] += versions[j].Size
		}
	}

	total := 0
	for owner, ids := range byOwner {
		if u, err := m.repo.User.GetByID(ctx, owner); err == nil {
			m.addStorage(ctx, u, -freed[owner])
		}
		if err := m.repo.File.Purge(ctx, owner, ids); err == nil {
			total += len(ids)
		}
	}
	return total, nil
}

// CleanupUploadTemp removes stale upload staging files left behind by upload
// sessions that expired without completing or cancelling. Returns how many files
// were removed.
func (m *Manager) CleanupUploadTemp() (int, error) {
	entries, err := os.ReadDir(m.tmpDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-uploadSessionTTL)
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(m.tmpDir, e.Name())) == nil {
			n++
		}
	}
	return n, nil
}

// splitByPolicy sorts the files with data on a policy into those whose current
// version lives there, or is already gone (lost whole with the policy), and
// those that only keep older versions there.
func (m *Manager) splitByPolicy(ctx context.Context, policyID uint) (whole, partial []model.File, err error) {
	files, err := m.repo.File.ListStoredOn(ctx, policyID)
	if err != nil {
		return nil, nil, err
	}
	for _, f := range files {
		if f.PrimaryEntityID == nil {
			whole = append(whole, f)
			continue
		}
		e, err := m.repo.Entity.GetByID(ctx, *f.PrimaryEntityID)
		switch {
		case errors.Is(err, repository.ErrNotFound) || err == nil && e.StoragePolicyID == policyID:
			whole = append(whole, f)
		case err != nil:
			return nil, nil, err
		default:
			partial = append(partial, f)
		}
	}
	return whole, partial, nil
}

// PolicyFileCount returns how many files PurgePolicy would delete outright.
func (m *Manager) PolicyFileCount(ctx context.Context, policyID uint) (int, error) {
	whole, _, err := m.splitByPolicy(ctx, policyID)
	return len(whole), err
}

// PurgePolicy permanently deletes everything stored on a storage policy, ahead
// of the policy's removal: files whose current version is there are purged
// whole, other files only lose the older versions stored there, and leftovers
// (thumbnails, abandoned uploads) are dropped. Physical deletes on an
// unreachable backend are best-effort; the rows go regardless. It returns the
// number of files purged.
func (m *Manager) PurgePolicy(ctx context.Context, policyID uint) (int, error) {
	whole, partial, err := m.splitByPolicy(ctx, policyID)
	if err != nil {
		return 0, err
	}
	users := map[uint]*model.User{}
	owner := func(id uint) (*model.User, error) {
		if u, ok := users[id]; ok {
			return u, nil
		}
		u, err := m.repo.User.GetByID(ctx, id)
		if errors.Is(err, repository.ErrNotFound) {
			// The owner's account is gone: its files are purged all the same, and
			// the quota update on the missing row is a no-op.
			u, err = &model.User{Base: model.Base{ID: id}}, nil
		}
		if err == nil {
			users[id] = u
		}
		return u, err
	}

	byOwner := map[uint][]uint{}
	for _, f := range whole {
		byOwner[f.OwnerID] = append(byOwner[f.OwnerID], f.ID)
	}
	purged := 0
	for id, ids := range byOwner {
		u, err := owner(id)
		if err != nil {
			return purged, err
		}
		if err := m.Purge(ctx, u, ids); err != nil {
			return purged, err
		}
		purged += len(ids)
	}

	for _, f := range partial {
		u, err := owner(f.OwnerID)
		if err != nil {
			return purged, err
		}
		versions, err := m.repo.Entity.ListVersions(ctx, f.ID)
		if err != nil {
			return purged, err
		}
		for _, v := range versions {
			if v.StoragePolicyID == policyID {
				m.removeEntity(ctx, v.ID)
				m.addStorage(ctx, u, -v.Size)
			}
		}
	}

	rest, err := m.repo.Entity.ListByPolicy(ctx, policyID)
	if err != nil {
		return purged, err
	}
	for _, e := range rest {
		m.removeEntity(ctx, e.ID)
	}
	return purged, nil
}

// DefaultPolicyID is the seeded storage policy: it cannot be deleted, and the
// groups of a deleted policy fall back to it.
const DefaultPolicyID = 1

// policyDeleteMu serialises policy deletions, so a scheduled resume never races
// the run an admin just started.
var policyDeleteMu sync.Mutex

// DeletePolicy removes a storage policy and everything stored on it: its groups
// move to the default policy, its data is purged (PurgePolicy), then the policy
// row goes. Every step is idempotent, so a run cut short (crash, restart,
// unreachable database) is finished by calling it again; the caller marks the
// policy deleting first so ResumePolicyDeletions does that on its own. It
// returns the number of files purged.
func (m *Manager) DeletePolicy(ctx context.Context, policyID uint) (int, error) {
	if policyID == DefaultPolicyID {
		return 0, errors.New("the default policy cannot be deleted")
	}
	policyDeleteMu.Lock()
	defer policyDeleteMu.Unlock()
	// Move groups first so no new upload lands on the policy while it is purged.
	if err := m.repo.Group.ReassignPolicy(ctx, policyID, DefaultPolicyID); err != nil {
		return 0, err
	}
	n, err := m.PurgePolicy(ctx, policyID)
	if err != nil {
		return n, err
	}
	return n, m.repo.Policy.Delete(ctx, policyID)
}

// ResumePolicyDeletions finishes every policy deletion that was started but
// never completed. It returns how many policies it deleted.
func (m *Manager) ResumePolicyDeletions(ctx context.Context) (int, error) {
	policies, err := m.repo.Policy.List(ctx)
	if err != nil {
		return 0, err
	}
	done := 0
	for i := range policies {
		if !policies[i].Deleting() {
			continue
		}
		if _, err := m.DeletePolicy(ctx, policies[i].ID); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}
