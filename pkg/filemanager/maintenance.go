package filemanager

import (
	"context"
	"os"
	"path/filepath"
	"time"
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
			u.Group = nil // detach association so Save doesn't touch group_id
			u.StorageUsed -= freed[owner]
			if u.StorageUsed < 0 {
				u.StorageUsed = 0
			}
			m.persistStorage(ctx, u)
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
