package filemanager

import (
	"context"
	"strings"

	"github.com/NhProGamer/orion-drive/model"
)

// The change journal powers WebDAV sync-collection (RFC 6578). Mutations record
// an upsert or a delete for the affected member's path; deletions are tombstones
// so a client syncing after a purge still learns the member is gone. Recording
// is best-effort — sync is an optimisation, never a correctness dependency.

// pathString builds a file's absolute virtual path ("/a/b/c"). It walks
// ancestors with the unscoped lookup so it still resolves mid-trash/purge;
// ancestor paths are memoised in cache across a batch.
func (m *Manager) pathString(ctx context.Context, ownerID uint, f *model.File, cache map[uint]string) string {
	segs := []string{f.Name}
	pid := f.ParentID
	for pid != nil {
		if base, ok := cache[*pid]; ok {
			return base + "/" + strings.Join(segs, "/")
		}
		p, err := m.repo.File.GetByIDUnscoped(ctx, ownerID, *pid)
		if err != nil {
			break
		}
		segs = append([]string{p.Name}, segs...)
		pid = p.ParentID
	}
	full := "/" + strings.Join(segs, "/")
	if f.ParentID != nil {
		cache[*f.ParentID] = strings.TrimSuffix(full, "/"+f.Name)
	}
	return full
}

// journalPath records a change for a path (best-effort).
func (m *Manager) journalPath(ctx context.Context, ownerID uint, path string, isDir, deleted bool) {
	if path == "" || path == "/" {
		return
	}
	_ = m.repo.FileChange.Append(ctx, &model.FileChange{
		UserID: ownerID, Path: path, IsDir: isDir, Deleted: deleted,
	})
}

// journalFile records a change for a file (computing its current path).
func (m *Manager) journalFile(ctx context.Context, ownerID uint, f *model.File, deleted bool) {
	m.journalPath(ctx, ownerID, m.pathString(ctx, ownerID, f, map[uint]string{}), f.IsFolder(), deleted)
}
