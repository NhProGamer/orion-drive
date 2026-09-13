// Package filemanager ties the repository and storage drivers together and
// exposes the high-level file operations used by the explorer service.
package filemanager

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/encrypt"
	"github.com/NhProGamer/orion-drive/pkg/queue"
	"github.com/NhProGamer/orion-drive/repository"
	"golang.org/x/sync/singleflight"
)

// Common errors surfaced to the API layer.
var (
	ErrConflict    = errors.New("a file with that name already exists here")
	ErrInvalidName = errors.New("invalid file name")
	ErrQuota       = errors.New("storage quota exceeded")
	ErrNotFound    = repository.ErrNotFound
	ErrNotAFolder  = errors.New("destination is not a folder")
	ErrLocked      = errors.New("file is locked")
)

// Manager coordinates logical files (repository) and physical storage (driver).
type Manager struct {
	repo    *repository.Repository
	cache   cache.Store
	tmpDir  string
	cipher  *encrypt.Cipher // nil when at-rest encryption is not configured
	queue   *queue.Queue
	archive ArchiveLimits // extraction safety limits (with defaults applied)
	// thumbSF collapses concurrent thumbnail requests for the same file into a
	// single generation; thumbSem caps concurrent external-tool generations so a
	// burst of video/office thumbnails can't exhaust CPU or request workers.
	thumbSF  singleflight.Group
	thumbSem chan struct{}
	// dedup is the content-addressed deduplication mode: "off", "user" or "global".
	dedup string
	// maxVersions caps kept versions per file; <= 0 means unlimited.
	maxVersions int
	// docThumb renders document thumbnails via an external WOPI document server
	// (Collabora/OnlyOffice) when LibreOffice is not installed locally. Injected
	// at bootstrap; nil disables the fallback.
	docThumb DocThumbFunc
}

// DocThumbFunc renders a JPEG thumbnail for a document file, given its buffered
// (decrypted) content path. Implementations talk to a Collabora/OnlyOffice
// document server. See Manager.SetDocThumbnailer.
type DocThumbFunc func(ctx context.Context, f *model.File, contentPath string) ([]byte, error)

// SetDocThumbnailer wires the external document-server thumbnail fallback.
func (m *Manager) SetDocThumbnailer(fn DocThumbFunc) { m.docThumb = fn }

// Ancestors returns the breadcrumb trail of folders leading to a file or folder
// owned by the user (used to rebuild navigation from a bookmarked URL).
func (m *Manager) Ancestors(ctx context.Context, user *model.User, id uint) ([]repository.Crumb, error) {
	return m.repo.File.Ancestors(ctx, user.ID, id)
}

// Deduplication modes for SetDedup.
const (
	DedupOff    = "off"
	DedupUser   = "user"
	DedupGlobal = "global"
)

// SetDedup selects the content-addressed deduplication mode. An unknown/empty
// value disables it.
func (m *Manager) SetDedup(mode string) {
	switch mode {
	case DedupUser, DedupGlobal:
		m.dedup = mode
	default:
		m.dedup = DedupOff
	}
}

// NewManager builds a Manager. tmpDir is where in-progress uploads are staged;
// cipher, when non-nil, encrypts objects for policies that request it; queue
// runs background archive jobs.
func NewManager(repo *repository.Repository, c cache.Store, tmpDir string, cipher *encrypt.Cipher, q *queue.Queue) *Manager {
	// Cap concurrent external thumbnail generations at half the cores (min 2).
	n := runtime.NumCPU() / 2
	if n < 2 {
		n = 2
	}
	return &Manager{
		repo: repo, cache: c, tmpDir: tmpDir, cipher: cipher, queue: q,
		archive:     ArchiveLimits{}.withDefaults(),
		thumbSem:    make(chan struct{}, n),
		maxVersions: defaultMaxVersions,
	}
}

// defaultMaxVersions is the version-retention cap applied when none is configured.
const defaultMaxVersions = 10

// SetMaxVersions sets the per-file version-retention cap. 0 keeps the built-in
// default; a negative value keeps unlimited history.
func (m *Manager) SetMaxVersions(n int) {
	switch {
	case n == 0:
		m.maxVersions = defaultMaxVersions
	case n < 0:
		m.maxVersions = 0 // unlimited
	default:
		m.maxVersions = n
	}
}

// SetArchiveLimits overrides the extraction safety limits; zero fields keep
// their default. Called at startup from configuration.
func (m *Manager) SetArchiveLimits(l ArchiveLimits) {
	m.archive = l.withDefaults()
}

// policySettings is the typed view of StoragePolicy.Settings used here.
type policySettings struct {
	Encrypt bool `json:"encrypt"`
}

// policyEncrypts reports whether a policy stores its objects encrypted.
func (m *Manager) policyEncrypts(p *model.StoragePolicy) bool {
	var s policySettings
	_ = p.Settings.Unmarshal(&s)
	return s.Encrypt
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

// TempFile creates a temporary file in OrionDrive's on-disk staging directory
// (not the system /tmp, which is often RAM-backed and would blow up buffering a
// large upload). Orphans here are swept by CleanupUploadTemp. The caller closes
// and removes the file.
func (m *Manager) TempFile(pattern string) (*os.File, error) {
	if err := os.MkdirAll(m.tmpDir, 0o755); err != nil {
		return nil, err
	}
	return os.CreateTemp(m.tmpDir, pattern)
}

// SetModified overrides a file's modification time (owner-scoped). Used to honour
// a client-supplied WebDAV X-OC-Mtime so sync clients keep the original mtime.
func (m *Manager) SetModified(ctx context.Context, user *model.User, id uint, t time.Time) error {
	return m.repo.File.SetModifiedTime(ctx, user.ID, id, t)
}

// FolderSize returns the total size of every non-trashed file nested under the
// folder, recursively. It walks the subtree one level at a time (owner-scoped);
// a plain file returns its own size.
func (m *Manager) FolderSize(ctx context.Context, user *model.User, id uint) (int64, error) {
	root, err := m.repo.File.GetByID(ctx, user.ID, id)
	if err != nil {
		return 0, err
	}
	if !root.IsFolder() {
		return root.Size, nil
	}
	var total int64
	level := []uint{id}
	// Depth bound guards against pathological or cyclic parent chains.
	for depth := 0; depth < 4096 && len(level) > 0; depth++ {
		kids, err := m.repo.File.ChildrenOfMany(ctx, user.ID, level)
		if err != nil {
			return 0, err
		}
		var next []uint
		for i := range kids {
			if kids[i].IsFolder() {
				next = append(next, kids[i].ID)
			} else {
				total += kids[i].Size
			}
		}
		level = next
	}
	return total, nil
}

// ListTrashed returns the user's recycle-bin contents.
func (m *Manager) ListTrashed(ctx context.Context, user *model.User) ([]model.File, error) {
	return m.repo.File.ListTrashed(ctx, user.ID)
}

// ListAllFiles returns every non-trashed file owned by the user (for the
// storage-usage view).
func (m *Manager) ListAllFiles(ctx context.Context, user *model.User) ([]model.File, error) {
	return m.repo.File.ListAllFiles(ctx, user.ID)
}

// Search returns non-trashed files matching query.
// Search returns the files matching the filters and whether more results exist
// past the requested page. When f.Limit > 0 the search is paginated: the caller
// gets at most f.Limit raw rows (advance f.Offset by f.Limit for the next page),
// and hasMore reports whether a further row exists. hasMore is measured on raw
// DB rows — before the extension-based kind filter — so paging stays aligned to
// the SQL offset even when a page's category filter drops some rows.
func (m *Manager) Search(ctx context.Context, user *model.User, f repository.SearchFilters, kind string) (files []model.File, hasMore bool, err error) {
	limit := f.Limit
	if limit > 0 {
		f.Limit = limit + 1 // over-fetch one to detect a next page
	}
	files, err = m.repo.File.Search(ctx, user.ID, f)
	if err != nil {
		return nil, false, err
	}
	if limit > 0 && len(files) > limit {
		hasMore = true
		files = files[:limit] // drop the probe row; it belongs to the next page
	}
	// Category filtering depends on the extension, so it runs here rather than
	// in SQL. A kind filter implies files (folders have no category).
	if kind != "" {
		out := files[:0]
		for _, fl := range files {
			if !fl.IsFolder() && matchesKind(fl.Name, kind) {
				out = append(out, fl)
			}
		}
		files = out
	}
	return files, hasMore, nil
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
	m.journalFile(ctx, user.ID, f, false)
	return f, nil
}

// EnsureFolderPath resolves (creating as needed) the chain of folders named by
// segments under parentID and returns the leaf folder. Existing folders are
// reused, so it is idempotent — the basis for folder uploads, where each file's
// directory chain must exist before the file is stored. A segment that collides
// with an existing non-folder file is a conflict.
func (m *Manager) EnsureFolderPath(ctx context.Context, user *model.User, parentID *uint, segments []string) (*model.File, error) {
	cur := parentID
	var leaf *model.File
	for _, seg := range segments {
		existing, err := m.repo.File.FindChildByName(ctx, user.ID, cur, seg)
		switch {
		case err == nil:
			if !existing.IsFolder() {
				return nil, ErrConflict
			}
			leaf = existing
		case errors.Is(err, ErrNotFound):
			f, cerr := m.CreateFolder(ctx, user, cur, seg)
			if errors.Is(cerr, ErrConflict) {
				// A concurrent request created it meanwhile — reuse that one.
				f, cerr = m.repo.File.FindChildByName(ctx, user.ID, cur, seg)
			}
			if cerr != nil {
				return nil, cerr
			}
			leaf = f
		default:
			return nil, err
		}
		id := leaf.ID
		cur = &id
	}
	if leaf == nil {
		return nil, ErrInvalidName
	}
	return leaf, nil
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
	if f.IsLocked() {
		return nil, ErrLocked
	}
	if existing, err := m.repo.File.FindChildByName(ctx, user.ID, f.ParentID, newName); err == nil && existing.ID != id {
		return nil, ErrConflict
	}
	oldPath := m.pathString(ctx, user.ID, f, map[uint]string{})
	f.Name = newName
	if err := m.repo.File.Update(ctx, f); err != nil {
		return nil, err
	}
	// The old href is gone, the new one appears.
	m.journalPath(ctx, user.ID, oldPath, f.IsFolder(), true)
	m.journalFile(ctx, user.ID, f, false)
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
		if f.IsLocked() {
			return ErrLocked
		}
		if destParentID != nil && *destParentID == id {
			return errors.New("cannot move a folder into itself")
		}
		if existing, err := m.repo.File.FindChildByName(ctx, user.ID, destParentID, f.Name); err == nil && existing.ID != id {
			return ErrConflict
		}
		oldPath := m.pathString(ctx, user.ID, f, map[uint]string{})
		f.ParentID = destParentID
		if err := m.repo.File.Update(ctx, f); err != nil {
			return err
		}
		m.journalPath(ctx, user.ID, oldPath, f.IsFolder(), true)
		m.journalFile(ctx, user.ID, f, false)
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

// Trash moves files to the recycle bin. Locked files are refused.
func (m *Manager) Trash(ctx context.Context, user *model.User, ids []uint) error {
	type member struct {
		path  string
		isDir bool
	}
	var members []member
	for _, id := range ids {
		f, err := m.repo.File.GetByID(ctx, user.ID, id)
		if err != nil {
			return err
		}
		if f.IsLocked() {
			return ErrLocked
		}
		members = append(members, member{m.pathString(ctx, user.ID, f, map[uint]string{}), f.IsFolder()})
	}
	// Trashing a folder trashes its whole subtree, so nothing is left orphaned
	// (unreachable but not in the recycle bin).
	all, err := m.expandSubtrees(ctx, user.ID, ids, false)
	if err != nil {
		return err
	}
	if err := m.repo.File.Trash(ctx, user.ID, all); err != nil {
		return err
	}
	// Journal the removals (top-level; a removed collection implies its subtree).
	for _, mm := range members {
		m.journalPath(ctx, user.ID, mm.path, mm.isDir, true)
	}
	return nil
}

// Restore returns files from the recycle bin.
func (m *Manager) Restore(ctx context.Context, user *model.User, ids []uint) error {
	all, err := m.expandSubtrees(ctx, user.ID, ids, true)
	if err != nil {
		return err
	}
	if err := m.repo.File.Restore(ctx, user.ID, all); err != nil {
		return err
	}
	// The restored members reappear (top-level; the client re-descends collections).
	for _, id := range ids {
		if f, err := m.repo.File.GetByID(ctx, user.ID, id); err == nil {
			m.journalFile(ctx, user.ID, f, false)
		}
	}
	return nil
}

// EmptyTrash permanently deletes every trashed file the user owns and returns
// how many were purged. It reuses Purge, so physical entities, shares, direct
// links and the quota counter are all cleaned up.
func (m *Manager) EmptyTrash(ctx context.Context, user *model.User) (int, error) {
	trashed, err := m.repo.File.ListTrashed(ctx, user.ID)
	if err != nil {
		return 0, err
	}
	if len(trashed) == 0 {
		return 0, nil
	}
	ids := make([]uint, len(trashed))
	for i := range trashed {
		ids[i] = trashed[i].ID
	}
	if err := m.Purge(ctx, user, ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// Purge permanently deletes files and their physical entities.
func (m *Manager) Purge(ctx context.Context, user *model.User, ids []uint) error {
	all, err := m.expandSubtrees(ctx, user.ID, ids, true)
	if err != nil {
		return err
	}
	// Capture the top-level members' paths before they are gone, to journal
	// tombstones (a purge is a hard delete — sync clients need the removal).
	type member struct {
		path  string
		isDir bool
	}
	var members []member
	for _, id := range ids {
		if f, err := m.repo.File.GetByIDUnscoped(ctx, user.ID, id); err == nil {
			members = append(members, member{m.pathString(ctx, user.ID, f, map[uint]string{}), f.IsFolder()})
		}
	}
	// Drop every share and direct link pointing into the subtree in one statement
	// each, rather than two deletes per file. Failures would leave dangling links
	// to purged files, so log rather than abort the purge.
	if err := m.repo.Share.DeleteByFiles(ctx, user.ID, all); err != nil {
		slog.Warn("purge: removing shares failed", "error", err)
	}
	if err := m.repo.DirectLink.DeleteByFiles(ctx, user.ID, all); err != nil {
		slog.Warn("purge: removing direct links failed", "error", err)
	}
	// One storage Handler per policy is reused across every entity in the batch.
	dcache := map[uint]driver.Handler{}
	var freed int64
	for _, id := range all {
		f, err := m.repo.File.GetByIDUnscoped(ctx, user.ID, id)
		if err != nil {
			continue
		}
		// Remove the file's thumbnail, if any (not counted against quota).
		if t, err := m.repo.Entity.GetThumb(ctx, f.ID); err == nil {
			m.removeEntityWith(ctx, t.ID, dcache)
		}
		// Remove every stored version of the file.
		versions, _ := m.repo.Entity.ListVersions(ctx, f.ID)
		if len(versions) == 0 && f.PrimaryEntityID != nil {
			m.removeEntityWith(ctx, *f.PrimaryEntityID, dcache)
			freed += f.Size
			continue
		}
		for i := range versions {
			m.removeEntityWith(ctx, versions[i].ID, dcache)
			freed += versions[i].Size
		}
	}
	m.addStorage(ctx, user, -freed)
	if err := m.repo.File.Purge(ctx, user.ID, all); err != nil {
		return err
	}
	for _, mm := range members {
		m.journalPath(ctx, user.ID, mm.path, mm.isDir, true)
	}
	return nil
}

// removeEntity deletes an entity row and its physical object. The physical
// object is removed only when no other entity still references the same Source,
// so a blob shared via deduplication survives until its last reference is gone.
func (m *Manager) removeEntity(ctx context.Context, entityID uint) {
	m.removeEntityWith(ctx, entityID, nil)
}

// removeEntityWith is removeEntity with an optional per-batch driver cache keyed
// by storage-policy id, so purging many entities of the same policy builds the
// storage Handler once instead of once per entity. Pass nil for a one-off.
func (m *Manager) removeEntityWith(ctx context.Context, entityID uint, dcache map[uint]driver.Handler) {
	e, err := m.repo.Entity.GetByID(ctx, entityID)
	if err != nil {
		return
	}
	shared := false
	if n, err := m.repo.Entity.CountBySource(ctx, e.Source, entityID); err == nil && n > 0 {
		shared = true
	}
	if !shared {
		if h, err := m.driverForPolicyCached(ctx, e.StoragePolicyID, dcache); err == nil {
			_, _ = h.Delete(ctx, e.Source)
		}
	}
	_ = m.repo.Entity.Delete(ctx, entityID)
}

// driverForPolicyCached resolves the storage Handler for a policy id, reusing
// dcache when provided (see removeEntityWith).
func (m *Manager) driverForPolicyCached(ctx context.Context, policyID uint, dcache map[uint]driver.Handler) (driver.Handler, error) {
	if dcache != nil {
		if h, ok := dcache[policyID]; ok {
			return h, nil
		}
	}
	policy, err := m.repo.Policy.GetByID(ctx, policyID)
	if err != nil {
		return nil, err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return nil, err
	}
	if dcache != nil {
		dcache[policyID] = h
	}
	return h, nil
}

// collectSubtree returns id plus the ids of everything nested under it. With
// unscoped=true it also descends through trashed rows (for restore/purge of a
// tree already in the recycle bin).
func (m *Manager) collectSubtree(ctx context.Context, ownerID, id uint, unscoped bool) ([]uint, error) {
	ids := []uint{id}
	// Walk level by level, batching all folders of a depth into one query
	// (WHERE parent_id IN ...) instead of one query per folder. Depth of the
	// tree, not its node count, bounds the number of round-trips.
	level := []uint{id}
	for len(level) > 0 {
		var (
			kids []model.File
			err  error
		)
		if unscoped {
			kids, err = m.repo.File.ChildrenOfManyUnscoped(ctx, ownerID, level)
		} else {
			kids, err = m.repo.File.ChildrenOfMany(ctx, ownerID, level)
		}
		if err != nil {
			return nil, err
		}
		var next []uint
		for i := range kids {
			ids = append(ids, kids[i].ID)
			if kids[i].IsFolder() {
				next = append(next, kids[i].ID)
			}
		}
		level = next
	}
	return ids, nil
}

// expandSubtrees turns a set of ids into that set plus every descendant, with
// duplicates removed.
func (m *Manager) expandSubtrees(ctx context.Context, ownerID uint, ids []uint, unscoped bool) ([]uint, error) {
	seen := map[uint]bool{}
	var out []uint
	for _, id := range ids {
		sub, err := m.collectSubtree(ctx, ownerID, id, unscoped)
		if err != nil {
			return nil, err
		}
		for _, s := range sub {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out, nil
}

// Capacity returns used and total bytes for the user's quota. Used is the
// maintained StorageUsed counter, which reflects every stored version (and
// trashed files still occupying space), not just the current version sizes.
func (m *Manager) Capacity(ctx context.Context, user *model.User) (used, total int64, err error) {
	used = user.StorageUsed
	if used < 0 {
		used = 0
	}
	if user.Group != nil {
		total = user.Group.MaxStorage
	}
	return used, total, nil
}

// addStorage atomically adjusts the user's storage counter by delta in the
// database (avoiding the read-modify-write race under concurrent operations) and
// mirrors the change on the in-memory user so it stays coherent within the
// request. A failed write only causes quota drift (not a failed operation), so
// it is logged rather than propagated — but it must never vanish silently.
func (m *Manager) addStorage(ctx context.Context, user *model.User, delta int64) {
	user.StorageUsed += delta
	if user.StorageUsed < 0 {
		user.StorageUsed = 0
	}
	if err := m.repo.User.AddStorage(ctx, user.ID, delta); err != nil {
		slog.Error("storage counter update failed; quota may drift", "user_id", user.ID, "error", err)
	}
}

// DownloadTarget is how a file's content should be delivered: either a direct
// URL to redirect the client to (URL != ""), or a stream for OrionDrive to
// serve itself (Stream != nil, already rate-limited).
type DownloadTarget struct {
	File   *model.File
	URL    string
	Stream driver.ReadSeekCloser
}

// Download resolves how to deliver a file. If the backend can hand out a direct
// (e.g. presigned) URL, the client is sent there; otherwise the content is
// streamed through OrionDrive, throttled to the group's download speed limit.
func (m *Manager) Download(ctx context.Context, user *model.User, id uint) (*DownloadTarget, error) {
	f, err := m.repo.File.GetByIDUnscoped(ctx, user.ID, id)
	if err != nil {
		return nil, err
	}
	if f.IsFolder() || f.PrimaryEntityID == nil {
		return nil, errors.New("file has no content")
	}
	e, err := m.repo.Entity.GetByID(ctx, *f.PrimaryEntityID)
	if err != nil {
		return nil, err
	}
	policy, err := m.repo.Policy.GetByID(ctx, e.StoragePolicyID)
	if err != nil {
		return nil, err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return nil, err
	}

	var speed int64
	if user.Group != nil {
		speed = user.Group.SpeedLimit
	}

	// Encrypted objects must be decrypted by OrionDrive as it streams them, so
	// they never use a direct provider URL.
	if e.Encrypted() {
		if m.cipher == nil {
			return nil, errors.New("cannot decrypt: no encryption key configured")
		}
		iv, err := base64.StdEncoding.DecodeString(e.DecodeProps().IV)
		if err != nil {
			return nil, err
		}
		rc, err := h.Open(ctx, e.Source)
		if err != nil {
			return nil, err
		}
		dec, err := m.cipher.DecryptReadSeeker(rc, iv)
		if err != nil {
			_ = rc.Close()
			return nil, err
		}
		return &DownloadTarget{File: f, Stream: newThrottledReader(dec, speed)}, nil
	}

	// Prefer a direct provider URL when the backend supports it.
	opts := driver.SourceOptions{Expire: time.Hour, DownloadFilename: f.Name}
	if url, err := h.Source(ctx, e.Source, opts); err == nil && url != "" {
		return &DownloadTarget{File: f, URL: url}, nil
	}

	rc, err := h.Open(ctx, e.Source)
	if err != nil {
		return nil, err
	}
	return &DownloadTarget{File: f, Stream: newThrottledReader(rc, speed)}, nil
}

// commitContent stores reader (size bytes) into the policy backend and records
// it, either as a new version of targetFileID (when non-nil) or as a new file
// named name under parentID. It creates the Entity + File rows and updates the
// user's used storage. The caller must have resolved policy/h and already run
// the conflict, lock and quota checks. Shared by resumable uploads and WebDAV.
func (m *Manager) commitContent(ctx context.Context, user *model.User, parentID *uint, name string, targetFileID *uint, policy *model.StoragePolicy, h driver.Handler, reader io.Reader, size int64) (*model.File, error) {
	source := newSourcePath(user.ID, name)

	// Hash the plaintext as it streams past (before any at-rest encryption), so
	// the content hash is stable regardless of encryption/IV. No extra pass.
	hasher := sha256.New()
	reader = io.TeeReader(reader, hasher)

	// Encrypt at rest when the policy requests it (local-serve backends only,
	// since encrypted bytes must be decrypted by OrionDrive on the way out).
	var entityProps model.JSON
	if m.policyEncrypts(policy) {
		if m.cipher == nil {
			return nil, errors.New("policy requests encryption but no encryption key is configured")
		}
		if !h.Capabilities().LocalServe {
			return nil, errors.New("encryption is only supported on local-serve storage policies")
		}
		enc, iv, err := m.cipher.EncryptReader(reader)
		if err != nil {
			return nil, err
		}
		reader = enc
		entityProps = model.MustJSON(model.EntityProps{IV: base64.StdEncoding.EncodeToString(iv)})
	}

	if err := h.Put(ctx, source, reader, size); err != nil {
		return nil, err
	}

	hashHex := hex.EncodeToString(hasher.Sum(nil))
	// Content-addressed deduplication: when enabled and the policy is unencrypted
	// (a random IV makes identical plaintext differ on disk), reuse an existing
	// blob with the same content and drop the duplicate we just wrote. The entity
	// row is still created (versions stay per-file) but shares the physical Source;
	// removeEntity only deletes a blob once its last reference is gone.
	if m.dedup != DedupOff && !m.policyEncrypts(policy) {
		var scope *uint
		if m.dedup == DedupUser {
			scope = &user.ID
		}
		if existing, derr := m.repo.Entity.FindDedupSource(ctx, hashHex, policy.ID, scope); derr == nil && existing != "" && existing != source {
			_, _ = h.Delete(ctx, source) // discard the duplicate we just streamed
			source = existing
		}
	}

	return m.finalize(ctx, user, parentID, name, targetFileID, policy, source, size, hashHex, entityProps)
}

// finalize creates the Entity (and File or new version) rows for a blob that has
// already been written to storage at source, updates the owner's used storage and
// journals the change. Shared by the streaming (commitContent) and file-adopt
// (WriteFileFrom) commit paths.
func (m *Manager) finalize(ctx context.Context, user *model.User, parentID *uint, name string, targetFileID *uint, policy *model.StoragePolicy, source string, size int64, hashHex string, entityProps model.JSON) (*model.File, error) {
	entity := &model.Entity{
		Type:            model.EntityTypeVersion,
		Source:          source,
		Size:            size,
		Hash:            hashHex,
		ReferenceCount:  1,
		StoragePolicyID: policy.ID,
		CreatedByID:     user.ID,
		Props:           entityProps,
	}
	if err := m.repo.Entity.Create(ctx, entity); err != nil {
		return nil, err
	}

	var file *model.File
	if targetFileID != nil {
		// Add a new version to the existing file; the old versions are kept.
		f, err := m.repo.File.GetByID(ctx, user.ID, *targetFileID)
		if err != nil {
			return nil, err
		}
		file = f
		entity.FileID = &file.ID
		if err := m.repo.Entity.Update(ctx, entity); err != nil {
			return nil, err
		}
		file.PrimaryEntityID = &entity.ID
		file.Size = size
		file.StoragePolicyID = policy.ID
		if err := m.repo.File.Update(ctx, file); err != nil {
			return nil, err
		}
	} else {
		file = &model.File{
			Name:            name,
			Type:            model.FileTypeFile,
			OwnerID:         user.ID,
			ParentID:        parentID,
			PrimaryEntityID: &entity.ID,
			Size:            size,
			StoragePolicyID: policy.ID,
		}
		if err := m.repo.File.Create(ctx, file); err != nil {
			return nil, err
		}
		entity.FileID = &file.ID
		if err := m.repo.Entity.Update(ctx, entity); err != nil {
			return nil, err
		}
	}

	m.addStorage(ctx, user, size)
	m.pruneVersions(ctx, user, file)
	m.journalFile(ctx, user.ID, file, false) // new file or new version → upsert
	return file, nil
}

// WriteFile stores reader (size bytes) as a file named name under parentID for
// the user, streaming straight through in one shot (used by WebDAV PUT). If a
// non-folder file with that name already exists it gains a new version; a
// folder with the same name conflicts. The group quota is enforced.
func (m *Manager) WriteFile(ctx context.Context, user *model.User, parentID *uint, name string, reader io.Reader, size int64) (*model.File, error) {
	name = strings.TrimSpace(name)
	if err := validateName(name); err != nil {
		return nil, err
	}
	if size < 0 {
		return nil, errors.New("invalid size")
	}
	if err := m.ensureParent(ctx, user, parentID); err != nil {
		return nil, err
	}

	var targetFileID *uint
	if existing, err := m.repo.File.FindChildByName(ctx, user.ID, parentID, name); err == nil {
		if existing.IsFolder() {
			return nil, ErrConflict
		}
		if existing.IsLocked() {
			return nil, ErrLocked
		}
		id := existing.ID
		targetFileID = &id
	}

	used, total, err := m.Capacity(ctx, user)
	if err != nil {
		return nil, err
	}
	if total > 0 && used+size > total {
		return nil, ErrQuota
	}

	policy, err := m.policyForUser(ctx, user)
	if err != nil {
		return nil, err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return nil, err
	}
	return m.commitContent(ctx, user, parentID, name, targetFileID, policy, h, reader, size)
}

// WriteFileFrom stores the already-staged file at tempPath as name under parentID
// for the user. When the backend can adopt a local file and the policy is not
// encrypted, the staged file is moved into storage (a metadata-only rename on the
// same filesystem) and hashed by reading it back from the page cache — one disk
// write instead of two (used by SFTP and WebDAV, which stage offset writes to a
// temp file). Otherwise it streams the temp file through the normal commit path.
// The staged file is consumed on the fast path; on the fallback path (and on a
// dedup hit) the caller remains responsible for removing it.
func (m *Manager) WriteFileFrom(ctx context.Context, user *model.User, parentID *uint, name, tempPath string, size int64) (*model.File, error) {
	name = strings.TrimSpace(name)
	if err := validateName(name); err != nil {
		return nil, err
	}
	if size < 0 {
		return nil, errors.New("invalid size")
	}
	if err := m.ensureParent(ctx, user, parentID); err != nil {
		return nil, err
	}

	var targetFileID *uint
	if existing, err := m.repo.File.FindChildByName(ctx, user.ID, parentID, name); err == nil {
		if existing.IsFolder() {
			return nil, ErrConflict
		}
		if existing.IsLocked() {
			return nil, ErrLocked
		}
		id := existing.ID
		targetFileID = &id
	}

	used, total, err := m.Capacity(ctx, user)
	if err != nil {
		return nil, err
	}
	if total > 0 && used+size > total {
		return nil, ErrQuota
	}

	policy, err := m.policyForUser(ctx, user)
	if err != nil {
		return nil, err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return nil, err
	}

	adopter, canAdopt := h.(driver.Adopter)
	if !canAdopt || m.policyEncrypts(policy) {
		// Fallback: stream the staged file through the copy-based commit path.
		f, oerr := os.Open(tempPath)
		if oerr != nil {
			return nil, oerr
		}
		defer f.Close()
		return m.commitContent(ctx, user, parentID, name, targetFileID, policy, h, io.LimitReader(f, size), size)
	}

	// Fast path: hash the staged bytes (page-cache read, no second write) then
	// move the file into storage.
	hashHex, err := hashFile(tempPath)
	if err != nil {
		return nil, err
	}
	source := newSourcePath(user.ID, name)
	if m.dedup != DedupOff {
		var scope *uint
		if m.dedup == DedupUser {
			scope = &user.ID
		}
		if existing, derr := m.repo.Entity.FindDedupSource(ctx, hashHex, policy.ID, scope); derr == nil && existing != "" && existing != source {
			// Identical content already stored: reuse it, leave the staged file for
			// the caller to discard, and skip the move entirely.
			return m.finalize(ctx, user, parentID, name, targetFileID, policy, existing, size, hashHex, nil)
		}
	}
	if err := adopter.Adopt(ctx, source, tempPath); err != nil {
		return nil, err
	}
	return m.finalize(ctx, user, parentID, name, targetFileID, policy, source, size, hashHex, nil)
}

// hashFile returns the hex SHA-256 of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// OpenContent returns a seekable reader over a file's current content, always
// streamed through OrionDrive and decrypted on the fly when needed. Used by
// WebDAV, which cannot follow a backend's direct (redirect) download URL.
func (m *Manager) OpenContent(ctx context.Context, f *model.File) (driver.ReadSeekCloser, error) {
	if f.IsFolder() || f.PrimaryEntityID == nil {
		return nil, errors.New("file has no content")
	}
	e, err := m.repo.Entity.GetByID(ctx, *f.PrimaryEntityID)
	if err != nil {
		return nil, err
	}
	policy, err := m.repo.Policy.GetByID(ctx, e.StoragePolicyID)
	if err != nil {
		return nil, err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return nil, err
	}
	rc, err := h.Open(ctx, e.Source)
	if err != nil {
		return nil, err
	}
	if !e.Encrypted() {
		return rc, nil
	}
	if m.cipher == nil {
		_ = rc.Close()
		return nil, errors.New("cannot decrypt: no encryption key configured")
	}
	iv, err := base64.StdEncoding.DecodeString(e.DecodeProps().IV)
	if err != nil {
		_ = rc.Close()
		return nil, err
	}
	dec, err := m.cipher.DecryptReadSeeker(rc, iv)
	if err != nil {
		_ = rc.Close()
		return nil, err
	}
	return dec, nil
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
