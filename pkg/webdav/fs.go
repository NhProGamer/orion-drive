// Package webdav exposes a user's OrionDrive files over WebDAV. It adapts the
// virtual filesystem (repository + storage driver) to golang.org/x/net/webdav's
// FileSystem interface, and authenticates clients with dedicated WebDAV
// credentials (see handler.go) rather than the OIDC session.
package webdav

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
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
	// Note: getetag is served from OpenFile(O_RDONLY).Stat() (see OpenFile), so we
	// don't pay an entity lookup here on the many existence-check Stat calls.
	return newInfo(target), nil
}

// OpenFile opens a file for reading, a directory for listing, or a path for
// writing (PUT). Write intent buffers to a temp file and commits on Close.
func (f *FS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (xwebdav.File, error) {
	user := userFromCtx(ctx)
	if user == nil {
		return nil, os.ErrPermission
	}

	// Write intent (PUT) sets O_WRONLY or O_CREATE/O_TRUNC. A plain O_RDWR (used
	// by PROPPATCH) must NOT be treated as a write, or it would truncate the file;
	// it falls through to the read/dir path, which carries the dead-prop handler.
	if flag&os.O_WRONLY != 0 || flag&(os.O_CREATE|os.O_TRUNC) != 0 {
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
	// x/net/webdav reads properties (incl. getetag) via OpenFile(O_RDONLY).Stat(),
	// so the content-hash ETag must live on this info, not just FS.Stat's.
	info := newInfo(target)
	if target.PrimaryEntityID != nil {
		if e, err := f.repo.Entity.GetByID(ctx, *target.PrimaryEntityID); err == nil {
			info.etag = e.Hash
		}
	}
	rc, err := f.mgr.OpenContent(ctx, target)
	if err != nil {
		return nil, mapErr(err)
	}
	return &readFile{davNode: davNode{fs: f, ctx: ctx, user: user, target: target}, rc: rc, info: info}, nil
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
	// Batch-load the children's content hashes (one query) so each entry gets a
	// strong ETag without a per-file lookup.
	entIDs := make([]uint, 0, len(kids))
	for i := range kids {
		if kids[i].PrimaryEntityID != nil {
			entIDs = append(entIDs, *kids[i].PrimaryEntityID)
		}
	}
	hashes, _ := f.repo.Entity.HashesByIDs(ctx, entIDs)
	infos := make([]fs.FileInfo, 0, len(kids))
	for i := range kids {
		info := newInfo(&kids[i])
		if kids[i].PrimaryEntityID != nil {
			info.etag = hashes[*kids[i].PrimaryEntityID]
		}
		infos = append(infos, info)
	}
	return &dirFile{davNode: davNode{fs: f, ctx: ctx, user: user, target: target}, info: newInfo(target), children: infos}, nil
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
	etag    string // content hash of the primary entity, if known
}

func (fi *fileInfo) Name() string       { return fi.name }
func (fi *fileInfo) Size() int64        { return fi.size }
func (fi *fileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *fileInfo) ModTime() time.Time { return fi.modTime }
func (fi *fileInfo) IsDir() bool        { return fi.isDir }
func (fi *fileInfo) Sys() any           { return nil }

// ETag implements webdav.ETager: a strong, content-addressed ETag from the
// object's SHA-256 when known, so identical content yields the same ETag and
// conditional requests are exact. Falls back to x/net/webdav's default
// (mtime+size) when no hash is stored (e.g. pre-hash files, folders).
func (fi *fileInfo) ETag(context.Context) (string, error) {
	if fi.etag != "" {
		return `"` + fi.etag + `"`, nil
	}
	return "", xwebdav.ErrNotImplemented
}

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

// --- Dead (custom) properties + RFC 4331 quota properties --------------------
//
// x/net/webdav calls DeadProps/Patch on the File returned by OpenFile, so both
// regular files and collections implement webdav.DeadPropsHolder through the
// embedded davNode. Custom properties set via PROPPATCH are persisted in the
// File's Props JSON (namespaced under "dav"); collections additionally expose
// computed quota-used-bytes / quota-available-bytes so clients show free space.

// davProp is one stored dead property.
type davProp struct {
	Space    string `json:"s"`
	Local    string `json:"l"`
	Lang     string `json:"lang,omitempty"`
	InnerXML []byte `json:"x"`
}

// davNode gives a WebDAV File its dead-property behaviour. target is nil for the
// drive root (a collection with no File row, so it cannot persist props).
type davNode struct {
	fs     *FS
	ctx    context.Context
	user   *model.User
	target *model.File
}

func (n davNode) isCollection() bool { return n.target == nil || n.target.IsFolder() }

func isQuotaProp(name xml.Name) bool {
	return name.Space == "DAV:" && (name.Local == "quota-available-bytes" || name.Local == "quota-used-bytes")
}

// DeadProps returns the stored custom properties plus, for collections, the
// computed quota properties.
func (n davNode) DeadProps() (map[xml.Name]xwebdav.Property, error) {
	props := map[xml.Name]xwebdav.Property{}
	if n.target != nil {
		for _, p := range decodeDavProps(n.target) {
			nm := xml.Name{Space: p.Space, Local: p.Local}
			props[nm] = xwebdav.Property{XMLName: nm, Lang: p.Lang, InnerXML: p.InnerXML}
		}
	}
	if n.isCollection() {
		if used, total, err := n.fs.mgr.Capacity(n.ctx, n.user); err == nil {
			add := func(local string, v int64) {
				nm := xml.Name{Space: "DAV:", Local: local}
				props[nm] = xwebdav.Property{XMLName: nm, InnerXML: []byte(strconv.FormatInt(v, 10))}
			}
			if total > 0 { // only advertise availability when the quota is bounded
				avail := total - used
				if avail < 0 {
					avail = 0
				}
				add("quota-available-bytes", avail)
			}
			add("quota-used-bytes", used)
		}
	}
	return props, nil
}

// Patch applies a PROPPATCH: sets/removes custom properties on the File. Quota
// properties are computed and read-only; the root has no File row to store on.
func (n davNode) Patch(patches []xwebdav.Proppatch) ([]xwebdav.Propstat, error) {
	if n.target == nil {
		return failPatch(patches, http.StatusForbidden), nil
	}
	for _, patch := range patches {
		for _, prop := range patch.Props {
			if isQuotaProp(prop.XMLName) {
				return failPatch(patches, http.StatusForbidden), nil
			}
		}
	}
	// Reload for the freshest Props, then apply and persist.
	f, err := n.fs.repo.File.GetByIDUnscoped(n.ctx, n.user.ID, n.target.ID)
	if err != nil {
		return nil, err
	}
	m := map[xml.Name]davProp{}
	for _, p := range decodeDavProps(f) {
		m[xml.Name{Space: p.Space, Local: p.Local}] = p
	}
	var ok []xwebdav.Property
	for _, patch := range patches {
		for _, prop := range patch.Props {
			if patch.Remove {
				delete(m, prop.XMLName)
			} else {
				m[prop.XMLName] = davProp{Space: prop.XMLName.Space, Local: prop.XMLName.Local, Lang: prop.Lang, InnerXML: prop.InnerXML}
			}
			ok = append(ok, xwebdav.Property{XMLName: prop.XMLName})
		}
	}
	list := make([]davProp, 0, len(m))
	for _, p := range m {
		list = append(list, p)
	}
	setDavProps(f, list)
	if err := n.fs.repo.File.Update(n.ctx, f); err != nil {
		return nil, err
	}
	return []xwebdav.Propstat{{Props: ok, Status: http.StatusOK}}, nil
}

// failPatch reports the given status for every property in patches.
func failPatch(patches []xwebdav.Proppatch, status int) []xwebdav.Propstat {
	var props []xwebdav.Property
	for _, patch := range patches {
		for _, prop := range patch.Props {
			props = append(props, xwebdav.Property{XMLName: prop.XMLName})
		}
	}
	return []xwebdav.Propstat{{Props: props, Status: status}}
}

// decodeDavProps reads the stored dead properties from a File's Props JSON.
func decodeDavProps(f *model.File) []davProp {
	var wrap map[string]json.RawMessage
	if f.Props.Unmarshal(&wrap) != nil || wrap == nil {
		return nil
	}
	raw, ok := wrap["dav"]
	if !ok {
		return nil
	}
	var list []davProp
	_ = json.Unmarshal(raw, &list)
	return list
}

// setDavProps writes the dead properties into a File's Props JSON, preserving
// any other keys (e.g. provenance) already stored there.
func setDavProps(f *model.File, list []davProp) {
	wrap := map[string]json.RawMessage{}
	_ = f.Props.Unmarshal(&wrap)
	if wrap == nil {
		wrap = map[string]json.RawMessage{}
	}
	if len(list) == 0 {
		delete(wrap, "dav")
	} else {
		b, _ := json.Marshal(list)
		wrap["dav"] = b
	}
	f.Props = model.MustJSON(wrap)
}

// readFile is a read-only regular file backed by a seekable content stream.
type readFile struct {
	davNode
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
	davNode
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

var errShortUpload = errors.New("upload shorter than the declared length")

// writeFile handles a PUT so quota, versioning and at-rest encryption all apply.
// When the client declares a Content-Length (the common case) the body is
// streamed straight into storage and finalized only once the whole declared
// length has arrived — a cut/short upload aborts and leaves any previous version
// untouched (never commits a truncated file). With an unknown length (chunked
// transfer) it falls back to buffering to a temp file committed on Close.
type writeFile struct {
	ctx      context.Context
	mgr      *filemanager.Manager
	user     *model.User
	parentID *uint
	name     string
	size     int64
	expected int64 // declared Content-Length, or -1 when unknown (chunked)
	closed   bool

	file *model.File // the committed file, for post-commit mtime override

	// Streaming path (expected >= 0): body is piped into WriteFile as it arrives.
	pw   *io.PipeWriter
	done chan error
	// Buffered path (expected < 0): body accumulated here, committed on Close.
	tmp *os.File
}

func newWriteFile(ctx context.Context, mgr *filemanager.Manager, user *model.User, parentID *uint, name string) (*writeFile, error) {
	w := &writeFile{ctx: ctx, mgr: mgr, user: user, parentID: parentID, name: name, expected: contentLengthFromCtx(ctx)}
	if w.expected >= 0 {
		pr, pw := io.Pipe()
		w.pw = pw
		w.done = make(chan error, 1)
		go func() {
			f, err := mgr.WriteFile(ctx, user, parentID, name, pr, w.expected)
			w.file = f
			_ = pr.CloseWithError(err) // unblock a pending Write if the commit failed early
			w.done <- err
		}()
		return w, nil
	}
	tmp, err := mgr.TempFile("orion-dav-*")
	if err != nil {
		return nil, err
	}
	w.tmp = tmp
	return w, nil
}

func (w *writeFile) Write(p []byte) (int, error) {
	if w.pw != nil {
		n, err := w.pw.Write(p)
		w.size += int64(n)
		return n, err
	}
	n, err := w.tmp.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *writeFile) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	if w.pw != nil {
		// A short body (cut connection) must never be committed: abort the pipe so
		// the in-flight WriteFile fails and no new version is created.
		if w.size != w.expected {
			_ = w.pw.CloseWithError(errShortUpload)
			if err := <-w.done; err != nil {
				return mapErr(err)
			}
			return mapErr(errShortUpload)
		}
		_ = w.pw.Close() // EOF → the commit finalizes
		if err := <-w.done; err != nil {
			return mapErr(err)
		}
		w.applyMtime()
		return nil
	}

	name := w.tmp.Name()
	_ = w.tmp.Close()     // flush and release the fd before the file is adopted/hashed
	defer os.Remove(name) // no-op once the staged file has been moved into storage
	f, err := w.mgr.WriteFileFrom(w.ctx, w.user, w.parentID, w.name, name, w.size)
	if err != nil {
		return mapErr(err)
	}
	w.file = f
	w.applyMtime()
	return nil
}

// applyMtime persists a client-supplied modification time (X-OC-Mtime) onto the
// just-committed file, so sync clients see their original mtime on the next
// PROPFIND (getlastmodified). Best-effort: a failure doesn't fail the PUT.
func (w *writeFile) applyMtime() {
	if w.file == nil {
		return
	}
	if t, ok := mtimeFromCtx(w.ctx); ok {
		_ = w.mgr.SetModified(w.ctx, w.user, w.file.ID, t)
	}
}

func (w *writeFile) Read([]byte) (int, error)         { return 0, os.ErrInvalid }
func (w *writeFile) Seek(int64, int) (int64, error)   { return 0, os.ErrInvalid }
func (w *writeFile) Readdir(int) ([]fs.FileInfo, error) { return nil, os.ErrInvalid }
func (w *writeFile) Stat() (fs.FileInfo, error) {
	return &fileInfo{name: w.name, size: w.size, mode: 0o644, modTime: time.Now()}, nil
}
