package sftpserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/pkg/sftp"
)

// handlers implements sftp's FileReader/FileWriter/FileCmder/FileLister over one
// authenticated user's drive. A fresh instance is used per SFTP session.
type handlers struct {
	mgr      *filemanager.Manager
	repo     *repository.Repository
	user     *model.User
	readOnly bool
}

func newHandlers(mgr *filemanager.Manager, repo *repository.Repository, user *model.User, readOnly bool) *handlers {
	return &handlers{mgr: mgr, repo: repo, user: user, readOnly: readOnly}
}

// splitPath cleans an absolute path into segments ("/" → none).
func splitPath(name string) []string {
	name = path.Clean("/" + strings.TrimSpace(name))
	if name == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(name, "/"), "/")
}

// resolve walks a path to the File it names (nil,nil = drive root).
func (h *handlers) resolve(ctx context.Context, p string) (*model.File, error) {
	var parentID *uint
	var cur *model.File
	for _, seg := range splitPath(p) {
		child, err := h.repo.File.FindChildByName(ctx, h.user.ID, parentID, seg)
		if err != nil {
			return nil, err
		}
		cur = child
		id := child.ID
		parentID = &id
	}
	return cur, nil
}

// resolveParent resolves a path's parent folder id and leaf name.
func (h *handlers) resolveParent(ctx context.Context, p string) (parentID *uint, leaf string, err error) {
	segs := splitPath(p)
	if len(segs) == 0 {
		return nil, "", os.ErrInvalid
	}
	leaf = segs[len(segs)-1]
	parent, err := h.resolve(ctx, "/"+strings.Join(segs[:len(segs)-1], "/"))
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

func sftpErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, filemanager.ErrNotFound), errors.Is(err, repository.ErrNotFound):
		return sftp.ErrSSHFxNoSuchFile
	case errors.Is(err, filemanager.ErrLocked), errors.Is(err, filemanager.ErrConflict):
		return sftp.ErrSSHFxPermissionDenied
	default:
		return err
	}
}

// Filelist handles List (readdir), Stat and Readlink.
func (h *handlers) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	ctx := context.Background()
	switch r.Method {
	case "List":
		target, err := h.resolve(ctx, r.Filepath)
		if err != nil {
			return nil, sftpErr(err)
		}
		if target != nil && !target.IsFolder() {
			return nil, sftp.ErrSSHFxNoSuchFile
		}
		var parentID *uint
		if target != nil {
			id := target.ID
			parentID = &id
		}
		kids, err := h.mgr.List(ctx, h.user, parentID)
		if err != nil {
			return nil, sftpErr(err)
		}
		infos := make([]os.FileInfo, 0, len(kids))
		for i := range kids {
			infos = append(infos, fileInfoOf(&kids[i], kids[i].Name))
		}
		return listerAt(infos), nil
	case "Stat", "Readlink":
		target, err := h.resolve(ctx, r.Filepath)
		if err != nil {
			return nil, sftpErr(err)
		}
		return listerAt([]os.FileInfo{fileInfoOf(target, path.Base(r.Filepath))}), nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

// Fileread serves a file's content as a (seekable) ReaderAt.
func (h *handlers) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	ctx := context.Background()
	target, err := h.resolve(ctx, r.Filepath)
	if err != nil {
		return nil, sftpErr(err)
	}
	if target == nil || target.IsFolder() {
		return nil, sftp.ErrSSHFxNoSuchFile
	}
	rc, err := h.mgr.OpenContent(ctx, target)
	if err != nil {
		return nil, sftpErr(err)
	}
	return &readerAt{rc: rc}, nil
}

// Filewrite buffers an upload to a temp file (SFTP writes at arbitrary offsets)
// and commits it through the Manager on close.
func (h *handlers) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	if h.readOnly {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	ctx := context.Background()
	parentID, leaf, err := h.resolveParent(ctx, r.Filepath)
	if err != nil {
		return nil, sftpErr(err)
	}
	tmp, err := os.CreateTemp("", "orion-sftp-*")
	if err != nil {
		return nil, err
	}
	return &writerAt{ctx: ctx, mgr: h.mgr, user: h.user, parentID: parentID, name: leaf, tmp: tmp}, nil
}

// Filecmd handles Mkdir, Remove/Rmdir, Rename, Setstat (mtime) and Symlink.
func (h *handlers) Filecmd(r *sftp.Request) error {
	if h.readOnly {
		return sftp.ErrSSHFxPermissionDenied
	}
	ctx := context.Background()
	switch r.Method {
	case "Mkdir":
		parentID, leaf, err := h.resolveParent(ctx, r.Filepath)
		if err != nil {
			return sftpErr(err)
		}
		_, err = h.mgr.CreateFolder(ctx, h.user, parentID, leaf)
		return sftpErr(err)
	case "Remove", "Rmdir":
		target, err := h.resolve(ctx, r.Filepath)
		if err != nil {
			return sftpErr(err)
		}
		if target == nil {
			return sftp.ErrSSHFxPermissionDenied // refuse to delete the drive root
		}
		return sftpErr(h.mgr.Trash(ctx, h.user, []uint{target.ID}))
	case "Rename":
		src, err := h.resolve(ctx, r.Filepath)
		if err != nil {
			return sftpErr(err)
		}
		if src == nil {
			return sftp.ErrSSHFxNoSuchFile
		}
		dstParent, leaf, err := h.resolveParent(ctx, r.Target)
		if err != nil {
			return sftpErr(err)
		}
		if !samePtr(src.ParentID, dstParent) {
			if err := h.mgr.Move(ctx, h.user, []uint{src.ID}, dstParent); err != nil {
				return sftpErr(err)
			}
		}
		if src.Name != leaf {
			if _, err := h.mgr.Rename(ctx, h.user, src.ID, leaf); err != nil {
				return sftpErr(err)
			}
		}
		return nil
	case "Setstat":
		// Honour a client-set mtime (scp -p, rsync); chmod/chown are no-ops on the
		// virtual filesystem.
		if attr := r.Attributes(); attr != nil && attr.Mtime != 0 {
			if target, err := h.resolve(ctx, r.Filepath); err == nil && target != nil {
				_ = h.mgr.SetModified(ctx, h.user, target.ID, time.Unix(int64(attr.Mtime), 0))
			}
		}
		return nil
	case "Symlink":
		return sftp.ErrSSHFxOpUnsupported
	}
	return sftp.ErrSSHFxOpUnsupported
}

func samePtr(a, b *uint) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// --- ReaderAt / WriterAt / ListerAt ----------------------------------------

// readerAt adapts a seekable content stream to io.ReaderAt (SFTP reads by
// offset). A mutex serialises the seek+read since the SFTP server may issue
// concurrent ReadAt calls on one handle.
type readerAt struct {
	mu  sync.Mutex
	rc  driver.ReadSeekCloser
	pos int64
}

func (r *readerAt) ReadAt(p []byte, off int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pos != off {
		if _, err := r.rc.Seek(off, io.SeekStart); err != nil {
			return 0, err
		}
		r.pos = off
	}
	n, err := io.ReadFull(r.rc, p)
	r.pos += int64(n)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF // ReadAt reports a short final read as EOF
	}
	return n, err
}

func (r *readerAt) Close() error { return r.rc.Close() }

// writerAt buffers offset writes to a temp file, committing on Close.
type writerAt struct {
	ctx      context.Context
	mgr      *filemanager.Manager
	user     *model.User
	parentID *uint
	name     string
	tmp      *os.File
	mu       sync.Mutex
	size     int64
	closed   bool
}

func (w *writerAt) WriteAt(p []byte, off int64) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.tmp.WriteAt(p, off)
	if end := off + int64(n); end > w.size {
		w.size = end
	}
	return n, err
}

func (w *writerAt) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
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
	_, err := w.mgr.WriteFile(w.ctx, w.user, w.parentID, w.name, io.LimitReader(w.tmp, w.size), w.size)
	return sftpErr(err)
}

type listerAt []os.FileInfo

func (l listerAt) ListAt(f []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(f, l[off:])
	if int(off)+n >= len(l) {
		return n, io.EOF
	}
	return n, nil
}

// --- os.FileInfo view of a File --------------------------------------------

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

// fileInfoOf builds an os.FileInfo for target (nil = drive root) with the given
// display name.
func fileInfoOf(target *model.File, name string) *fileInfo {
	if target == nil {
		return &fileInfo{name: name, mode: os.ModeDir | 0o755, modTime: time.Now(), isDir: true}
	}
	if target.IsFolder() {
		return &fileInfo{name: name, mode: os.ModeDir | 0o755, modTime: target.UpdatedAt, isDir: true}
	}
	return &fileInfo{name: name, size: target.Size, mode: 0o644, modTime: target.UpdatedAt}
}
