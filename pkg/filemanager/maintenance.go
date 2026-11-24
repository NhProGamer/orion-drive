package filemanager

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/NhProGamer/orion-drive/repository"
)

// report publishes a maintenance job's progress to fn, which may be nil.
func report(fn func(done, total int64), done, total int) {
	if fn != nil {
		fn(int64(done), int64(total))
	}
}

// PurgeExpiredTrash permanently deletes files that have been in the trash longer
// than retention, freeing their storage and adjusting each owner's usage. It
// returns the number of files purged. A retention of 0 disables the job.
//
// Each file is purged whole before the next starts, so cancelling ctx stops the
// job between two files and never leaves one half-deleted.
func (m *Manager) PurgeExpiredTrash(ctx context.Context, retention time.Duration, progress func(done, total int64)) (int, error) {
	if retention <= 0 {
		return 0, nil
	}
	before := time.Now().Add(-retention)
	files, err := m.repo.File.ListTrashedBefore(ctx, before)
	if err != nil || len(files) == 0 {
		return 0, err
	}
	// Per-file work runs to completion even once a stop is asked.
	work := context.WithoutCancel(ctx)
	users := map[uint]*model.User{}
	purged := 0
	for i := range files {
		if err := ctx.Err(); err != nil {
			return purged, err
		}
		report(progress, i, len(files))
		f := &files[i]
		// Drop share links / direct links / thumbnail for the file.
		_ = m.repo.Share.DeleteByFile(work, f.OwnerID, f.ID)
		_ = m.repo.DirectLink.DeleteByFile(work, f.OwnerID, f.ID)
		if t, err := m.repo.Entity.GetThumb(work, f.ID); err == nil {
			m.removeEntity(work, t.ID)
		}
		// Remove every stored version and account for the freed bytes.
		var freed int64
		versions, _ := m.repo.Entity.ListVersions(work, f.ID)
		if len(versions) == 0 && f.PrimaryEntityID != nil {
			m.removeEntity(work, *f.PrimaryEntityID)
			freed = f.Size
		}
		for j := range versions {
			m.removeEntity(work, versions[j].ID)
			freed += versions[j].Size
		}
		u, ok := users[f.OwnerID]
		if !ok {
			if u, _ = m.repo.User.GetByID(work, f.OwnerID); u != nil {
				users[f.OwnerID] = u
			}
		}
		if u != nil {
			m.addStorage(work, u, -freed)
		}
		if err := m.repo.File.Purge(work, f.OwnerID, []uint{f.ID}); err == nil {
			purged++
		}
	}
	report(progress, len(files), len(files))
	return purged, nil
}

// CleanupUploadTemp removes stale upload staging files left behind by upload
// sessions that expired without completing or cancelling. Returns how many files
// were removed; cancelling ctx stops it between two files.
func (m *Manager) CleanupUploadTemp(ctx context.Context, progress func(done, total int64)) (int, error) {
	entries, err := os.ReadDir(m.tmpDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-uploadSessionTTL)
	n := 0
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		report(progress, i, len(entries))
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
	report(progress, len(entries), len(entries))
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

// pingTimeout bounds a reachability probe of a storage backend: an offline node
// must not hold the admin's confirmation dialog for long.
const pingTimeout = 5 * time.Second

// ping probes h if it can tell whether it is reachable; other backends count
// as online.
func ping(ctx context.Context, h driver.Handler) error {
	p, ok := h.(driver.Pinger)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return p.Ping(ctx)
}

// PolicyOnline reports whether a policy's storage backend answers right now,
// i.e. whether deleting the policy would also erase the stored data.
func (m *Manager) PolicyOnline(ctx context.Context, policyID uint) bool {
	p, err := m.repo.Policy.GetByID(ctx, policyID)
	if err != nil {
		return false
	}
	h, err := m.driverForPolicy(p)
	return err == nil && ping(ctx, h) == nil
}

// departingStore wraps the backend of a policy being deleted, where physical
// deletes are best-effort: once the backend is offline (from the start, or
// after a delete fails) the remaining ones are skipped and counted, so an
// offline node costs nothing per object instead of a connection timeout each.
// Used by one PurgePolicy run at a time, so it needs no lock.
type departingStore struct {
	driver.Handler // nil when the backend could not even be built
	policyID       uint
	offline        bool
	skipped        int
}

func (d *departingStore) Delete(ctx context.Context, srcs ...string) ([]string, error) {
	if d.offline || d.Handler == nil {
		d.skipped += len(srcs)
		return srcs, nil
	}
	failed, err := d.Handler.Delete(ctx, srcs...)
	if err != nil {
		slog.Warn("policy deletion: storage unreachable, deleting metadata only from now on", "policy_id", d.policyID, "error", err)
		d.offline = true
		d.skipped += len(srcs)
	}
	return failed, err
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
// offline backend are skipped (see departingStore): only the metadata goes, and
// the objects stay on the node. It returns the number of files purged.
//
// Each file is purged whole before the next starts: cancelling ctx stops
// between two files, and the next call picks up what is left.
func (m *Manager) PurgePolicy(ctx context.Context, policyID uint, progress func(done, total int64)) (int, error) {
	whole, partial, err := m.splitByPolicy(ctx, policyID)
	if err != nil {
		return 0, err
	}
	work := context.WithoutCancel(ctx)
	total, done := len(whole)+len(partial), 0

	store := &departingStore{policyID: policyID}
	if p, err := m.repo.Policy.GetByID(ctx, policyID); err == nil {
		store.Handler, _ = m.driverForPolicy(p)
	}
	if store.Handler == nil || ping(ctx, store.Handler) != nil {
		store.offline = true
	}
	dcache := map[uint]driver.Handler{policyID: store}
	defer func() {
		if store.skipped > 0 {
			slog.Warn("policy deletion: objects left on the unreachable storage", "policy_id", policyID, "objects", store.skipped)
		}
	}()
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
		for _, fileID := range ids {
			if err := ctx.Err(); err != nil {
				return purged, err
			}
			if err := m.purge(work, u, []uint{fileID}, dcache); err != nil {
				return purged, err
			}
			purged++
			done++
			report(progress, done, total)
		}
	}

	for _, f := range partial {
		if err := ctx.Err(); err != nil {
			return purged, err
		}
		u, err := owner(f.OwnerID)
		if err != nil {
			return purged, err
		}
		versions, err := m.repo.Entity.ListVersions(work, f.ID)
		if err != nil {
			return purged, err
		}
		for _, v := range versions {
			if v.StoragePolicyID == policyID {
				m.removeEntityWith(work, v.ID, dcache)
				m.addStorage(work, u, -v.Size)
			}
		}
		done++
		report(progress, done, total)
	}

	rest, err := m.repo.Entity.ListByPolicy(work, policyID)
	if err != nil {
		return purged, err
	}
	for _, e := range rest {
		if err := ctx.Err(); err != nil {
			return purged, err
		}
		m.removeEntityWith(work, e.ID, dcache)
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
func (m *Manager) DeletePolicy(ctx context.Context, policyID uint, progress func(done, total int64)) (int, error) {
	if policyID == DefaultPolicyID {
		return 0, errors.New("the default policy cannot be deleted")
	}
	policyDeleteMu.Lock()
	defer policyDeleteMu.Unlock()
	// Move groups first so no new upload lands on the policy while it is purged.
	if err := m.repo.Group.ReassignPolicy(ctx, policyID, DefaultPolicyID); err != nil {
		return 0, err
	}
	n, err := m.PurgePolicy(ctx, policyID, progress)
	if err != nil {
		return n, err
	}
	return n, m.repo.Policy.Delete(ctx, policyID)
}

// ResumePolicyDeletions finishes every policy deletion that was started but
// never completed (or was stopped). It returns how many policies it deleted.
func (m *Manager) ResumePolicyDeletions(ctx context.Context, progress func(done, total int64)) (int, error) {
	policies, err := m.repo.Policy.List(ctx)
	if err != nil {
		return 0, err
	}
	done := 0
	for i := range policies {
		if !policies[i].Deleting() {
			continue
		}
		if _, err := m.DeletePolicy(ctx, policies[i].ID, progress); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}
