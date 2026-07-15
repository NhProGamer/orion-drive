// Package webdav exposes a user's OrionDrive files over WebDAV. It adapts the
// virtual filesystem (repository + storage driver) to golang.org/x/net/webdav's
// FileSystem interface, and authenticates clients with dedicated WebDAV
// credentials (see handler.go) rather than the OIDC session.
package webdav

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/NhProGamer/orion-drive/repository"
	xwebdav "golang.org/x/net/webdav"
)

// FS implements xwebdav.FileSystem over OrionDrive's virtual filesystem. It is a
// single shared instance; the authenticated user is carried in the request
// context (set by the auth handler) and read on every call.
type FS struct {
	mgr  *filemanager.Manager
	repo *repository.Repository
}

// NewFS builds a WebDAV FileSystem backed by mgr and repo.
func NewFS(mgr *filemanager.Manager, repo *repository.Repository) *FS {
	return &FS{mgr: mgr, repo: repo}
}

// splitPath cleans an absolute WebDAV name into its path segments ("/" → none).
func splitPath(name string) []string {
	name = path.Clean("/" + strings.TrimSpace(name))
	if name == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(name, "/"), "/")
}

// mapErr translates a filemanager/repository error into the os error values the
// webdav package understands (it inspects them with os.IsNotExist, etc.).
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, filemanager.ErrNotFound), errors.Is(err, repository.ErrNotFound):
		return os.ErrNotExist
	case errors.Is(err, filemanager.ErrConflict):
		return os.ErrExist
	case errors.Is(err, filemanager.ErrInvalidName):
		return os.ErrInvalid
	default:
		return err
	}
}

// resolve walks name segment by segment and returns the File it names. A nil
// File with a nil error means the drive root (which has no File row).
func (f *FS) resolve(ctx context.Context, user *model.User, name string) (*model.File, error) {
	var parentID *uint
	var cur *model.File
	for _, seg := range splitPath(name) {
		child, err := f.repo.File.FindChildByName(ctx, user.ID, parentID, seg)
		if err != nil {
			return nil, mapErr(err)
		}
		cur = child
		id := child.ID
		parentID = &id
	}
	return cur, nil
}

// resolveParent resolves name's parent folder and returns its ID (nil = root)
// and the leaf (last) segment.
func (f *FS) resolveParent(ctx context.Context, user *model.User, name string) (parentID *uint, leaf string, err error) {
	segs := splitPath(name)
	if len(segs) == 0 {
		return nil, "", os.ErrInvalid // the root has no parent
	}
	leaf = segs[len(segs)-1]
	parent, err := f.resolve(ctx, user, "/"+strings.Join(segs[:len(segs)-1], "/"))
	if err != nil {
		return nil, "", err
	}
	if parent != nil {
		if !parent.IsFolder() {
			return nil, "", os.ErrInvalid
		}
		id := parent.ID
		parentID = &id
	}
	return parentID, leaf, nil
}

// Mkdir creates a folder (MKCOL).
func (f *FS) Mkdir(ctx context.Context, name string, _ os.FileMode) error {
	user := userFromCtx(ctx)
	if user == nil {
		return os.ErrPermission
	}
	parentID, leaf, err := f.resolveParent(ctx, user, name)
	if err != nil {
		return err
	}
	_, err = f.mgr.CreateFolder(ctx, user, parentID, leaf)
	return mapErr(err)
}

// RemoveAll moves a file or folder to the recycle bin (DELETE).
func (f *FS) RemoveAll(ctx context.Context, name string) error {
	user := userFromCtx(ctx)
	if user == nil {
		return os.ErrPermission
	}
	target, err := f.resolve(ctx, user, name)
	if err != nil {
		return err
	}
	if target == nil {
		return os.ErrInvalid // refuse to delete the drive root
	}
	return mapErr(f.mgr.Trash(ctx, user, []uint{target.ID}))
}

// Rename moves and/or renames a file (MOVE).
func (f *FS) Rename(ctx context.Context, oldName, newName string) error {
	user := userFromCtx(ctx)
	if user == nil {
		return os.ErrPermission
	}
	src, err := f.resolve(ctx, user, oldName)
	if err != nil {
		return err
	}
	if src == nil {
		return os.ErrInvalid
	}
	dstParentID, leaf, err := f.resolveParent(ctx, user, newName)
	if err != nil {
		return err
	}
	if !samePtr(src.ParentID, dstParentID) {
		if err := f.mgr.Move(ctx, user, []uint{src.ID}, dstParentID); err != nil {
			return mapErr(err)
		}
	}
	if src.Name != leaf {
		if _, err := f.mgr.Rename(ctx, user, src.ID, leaf); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

// Stat returns metadata for a file (PROPFIND depth 0, HEAD).
func (f *FS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	user := userFromCtx(ctx)
	if user == nil {
		return nil, os.ErrPermission
	}
	target, err := f.resolve(ctx, user, name)
	if err != nil {
		return nil, err
	}
	return newInfo(target), nil
}

// OpenFile opens a file for reading, a directory for listing, or a path for
// writing (PUT). Write intent buffers to a temp file and commits on Close.
func (f *FS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (xwebdav.File, error) {
	user := userFromCtx(ctx)
	if user == nil {
		return nil, os.ErrPermission
	}

	if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		parentID, leaf, err := f.resolveParent(ctx, user, name)
		if err != nil {
			return nil, err
		}
		return newWriteFile(ctx, f.mgr, user, parentID, leaf)
	}

	target, err := f.resolve(ctx, user, name)
	if err != nil {
		return nil, err
	}
	if target == nil || target.IsFolder() {
		return f.openDir(ctx, user, target)
	}
	rc, err := f.mgr.OpenContent(ctx, target)
	if err != nil {
		return nil, mapErr(err)
	}
	return &readFile{rc: rc, info: newInfo(target)}, nil
}

// openDir builds a directory File whose Readdir lists target's children (or the
// root's children when target is nil).
func (f *FS) openDir(ctx context.Context, user *model.User, target *model.File) (xwebdav.File, error) {
	var parentID *uint
	if target != nil {
		id := target.ID
		parentID = &id
	}
	kids, err := f.mgr.List(ctx, user, parentID)
	if err != nil {
		return nil, mapErr(err)
	}
	infos := make([]fs.FileInfo, 0, len(kids))
	for i := range kids {
		infos = append(infos, newInfo(&kids[i]))
	}
	return &dirFile{info: newInfo(target), children: infos}, nil
}

// samePtr reports whether two *uint are equal (both nil, or same value).
func samePtr(a, b *uint) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// fileInfo is the os.FileInfo view of a File (or the drive root).
type fileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (fi *fileInfo) Name() string       { return fi.name }
func (fi *fileInfo) Size() int64        { return fi.size }
func (fi *fileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *fileInfo) ModTime() time.Time { return fi.modTime }
func (fi *fileInfo) IsDir() bool        { return fi.isDir }
func (fi *fileInfo) Sys() any           { return nil }

// newInfo builds a fileInfo for target; a nil target is the drive root.
func newInfo(target *model.File) *fileInfo {
	if target == nil {
		return &fileInfo{name: "/", mode: os.ModeDir | 0o755, modTime: time.Now(), isDir: true}
	}
	if target.IsFolder() {
		return &fileInfo{name: target.Name, mode: os.ModeDir | 0o755, modTime: target.UpdatedAt, isDir: true}
	}
	return &fileInfo{name: target.Name, size: target.Size, mode: 0o644, modTime: target.UpdatedAt}
}

// readFile is a read-only regular file backed by a seekable content stream.
type readFile struct {
	rc   driver.ReadSeekCloser
	info os.FileInfo
}

func (r *readFile) Read(p []byte) (int, error)          { return r.rc.Read(p) }
func (r *readFile) Seek(o int64, w int) (int64, error)  { return r.rc.Seek(o, w) }
func (r *readFile) Close() error                        { return r.rc.Close() }
func (r *readFile) Write([]byte) (int, error)           { return 0, os.ErrPermission }
func (r *readFile) Readdir(int) ([]fs.FileInfo, error)  { return nil, os.ErrInvalid }
func (r *readFile) Stat() (fs.FileInfo, error)          { return r.info, nil }

// dirFile is a directory: Readdir yields its children; content ops are invalid.
type dirFile struct {
	info     os.FileInfo
	children []fs.FileInfo
	off      int
}

func (d *dirFile) Read([]byte) (int, error)         { return 0, os.ErrInvalid }
func (d *dirFile) Seek(int64, int) (int64, error)   { return 0, nil }
func (d *dirFile) Write([]byte) (int, error)        { return 0, os.ErrPermission }
func (d *dirFile) Close() error                     { return nil }
func (d *dirFile) Stat() (fs.FileInfo, error)       { return d.info, nil }

// Readdir returns directory children with os.File semantics: count <= 0 returns
// everything remaining; a positive count returns up to that many, with io.EOF
// once exhausted.
func (d *dirFile) Readdir(count int) ([]fs.FileInfo, error) {
	if count <= 0 {
		rest := d.children[d.off:]
		d.off = len(d.children)
		return rest, nil
	}
	if d.off >= len(d.children) {
		return nil, io.EOF
	}
	end := d.off + count
	if end > len(d.children) {
		end = len(d.children)
	}
	batch := d.children[d.off:end]
	d.off = end
	return batch, nil
}

// writeFile buffers a PUT to a temp file and commits it through the manager on
// Close, so quota, versioning and at-rest encryption all apply.
type writeFile struct {
	ctx      context.Context
	mgr      *filemanager.Manager
	user     *model.User
	parentID *uint
	name     string
	tmp      *os.File
	size     int64
	closed   bool
}

func newWriteFile(ctx context.Context, mgr *filemanager.Manager, user *model.User, parentID *uint, name string) (*writeFile, error) {
	tmp, err := os.CreateTemp("", "orion-dav-*")
	if err != nil {
		return nil, err
	}
	return &writeFile{ctx: ctx, mgr: mgr, user: user, parentID: parentID, name: name, tmp: tmp}, nil
}

func (w *writeFile) Write(p []byte) (int, error) {
	n, err := w.tmp.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *writeFile) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	defer func() {
		_ = w.tmp.Close()
		_ = os.Remove(w.tmp.Name())
	}()
	if _, err := w.tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := w.mgr.WriteFile(w.ctx, w.user, w.parentID, w.name, io.LimitReader(w.tmp, w.size), w.size); err != nil {
		return mapErr(err)
	}
	return nil
}

func (w *writeFile) Read([]byte) (int, error)         { return 0, os.ErrInvalid }
func (w *writeFile) Seek(int64, int) (int64, error)   { return 0, os.ErrInvalid }
func (w *writeFile) Readdir(int) ([]fs.FileInfo, error) { return nil, os.ErrInvalid }
func (w *writeFile) Stat() (fs.FileInfo, error) {
	return &fileInfo{name: w.name, size: w.size, mode: 0o644, modTime: time.Now()}, nil
}
