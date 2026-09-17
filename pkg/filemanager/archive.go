package filemanager

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/archive"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
)

// maxTreeDepth bounds how deep an archive walk follows folders. The drive's
// tree cannot legitimately nest this far, so hitting it means a parent cycle
// left by a bug — which would otherwise recurse forever.
const maxTreeDepth = 64

// ErrArchiveTooDeep is returned when a tree nests past maxTreeDepth.
var ErrArchiveTooDeep = errors.New("folder tree is nested too deeply to archive")

// ErrArchiveFormat is returned when a caller asks for a format OrionDrive
// cannot produce.
var ErrArchiveFormat = errors.New("unsupported archive format")

// openContent returns a file's current content as a plain (decrypted) reader,
// regardless of the backend. Callers must Close the reader.
func (m *Manager) openContent(ctx context.Context, f *model.File) (io.ReadCloser, error) {
	if f.PrimaryEntityID == nil {
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
	if e.Encrypted() {
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
	return rc, nil
}

// openSeekable returns a file's content as a seekable, decrypted reader.
//
// It exists next to openContent because that one degrades its result to an
// io.ReadCloser, throwing away the seeking every backend actually provides —
// and seeking is exactly what lets an archive reader fetch an index without
// pulling the whole object.
func (m *Manager) openSeekable(ctx context.Context, f *model.File) (driver.ReadSeekCloser, error) {
	if f.PrimaryEntityID == nil {
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
	// AES-CTR keeps random access, so an encrypted archive is read the same way.
	dec, err := m.cipher.DecryptReadSeeker(rc, iv)
	if err != nil {
		_ = rc.Close()
		return nil, err
	}
	return dec, nil
}

// ctxReader aborts a read once the context is done.
//
// Cancelling a job only takes effect where the work looks at its context, and
// io.Copy over a multi-gigabyte member does not — so without this, stopping a
// compression would wait for the current file to finish.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// treeEntry is one member of an archive being produced: a file to write, or a
// folder that holds nothing and needs an explicit entry of its own.
type treeEntry struct {
	file  *model.File
	name  string // path inside the archive, slash-separated
	isDir bool
}

// walkTree visits the files and folders behind ids, depth-first, calling cb
// with the path each one takes inside the archive. It is the single traversal
// behind both the size pre-flight and the writers.
func (m *Manager) walkTree(ctx context.Context, user *model.User, ids []uint, cb func(treeEntry) error) error {
	for _, id := range ids {
		f, err := m.repo.File.GetByID(ctx, user.ID, id)
		if err != nil {
			return err
		}
		if err := m.walkNode(ctx, f, "", 0, cb); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) walkNode(ctx context.Context, f *model.File, prefix string, depth int, cb func(treeEntry) error) error {
	if depth > maxTreeDepth {
		return ErrArchiveTooDeep
	}
	name := path.Join(prefix, f.Name)
	if !f.IsFolder() {
		return cb(treeEntry{file: f, name: name})
	}
	children, err := m.repo.File.ListChildren(ctx, f.OwnerID, &f.ID)
	if err != nil {
		return err
	}
	if len(children) == 0 {
		// An empty folder only survives the round trip as an explicit entry.
		return cb(treeEntry{file: f, name: name, isDir: true})
	}
	for i := range children {
		if err := m.walkNode(ctx, &children[i], name, depth+1, cb); err != nil {
			return err
		}
	}
	return nil
}

// ArchivePlan is what an archive request would produce, measured before a byte
// is written.
type ArchivePlan struct {
	Entries int
	Bytes   int64
}

// PlanArchive measures the archive ids would produce and refuses it if it
// exceeds the configured limits.
//
// The point is to answer before the response has started: a streaming download
// cannot take back its headers, so an archive that turns out to be too large
// halfway through can only be abandoned mid-body — leaving the client with a
// truncated file behind a 200.
func (m *Manager) PlanArchive(ctx context.Context, user *model.User, ids []uint) (ArchivePlan, error) {
	if len(ids) == 0 {
		return ArchivePlan{}, errors.New("nothing to archive")
	}
	var plan ArchivePlan
	err := m.walkTree(ctx, user, ids, func(e treeEntry) error {
		plan.Entries++
		if plan.Entries > m.archive.MaxEntries {
			return ErrTooManyFiles
		}
		if !e.isDir {
			plan.Bytes += e.file.Size
			if plan.Bytes < 0 || plan.Bytes > m.archive.MaxUncompressed {
				return ErrArchiveTooLarge
			}
		}
		return nil
	})
	if err != nil {
		return ArchivePlan{}, err
	}
	return plan, nil
}

// ArchiveFormat resolves the format to produce: the requested one, or the
// configured default when empty. An unsupported request is an error rather than
// a silent fallback.
func (m *Manager) ArchiveFormat(requested string) (string, error) {
	if requested == "" {
		requested = m.archive.DefaultFormat
	}
	if !archive.CanCreate(requested) {
		return "", fmt.Errorf("%w: %q", ErrArchiveFormat, requested)
	}
	return requested, nil
}

// ArchiveLevel resolves the compression effort: the requested one, or the
// configured default when unset.
func (m *Manager) ArchiveLevel(requested int) int {
	if requested > 0 {
		return requested
	}
	return m.archive.Level
}

// ArchiveProgress is called after each member is written, with the running
// totals of members and bytes consumed from the source.
type ArchiveProgress func(entries int, bytes int64)

// WriteArchive streams an archive of the given files/folders (recursively) to w
// in the given format. onProgress may be nil.
//
// Callers are expected to have run PlanArchive first: once w has been written
// to, a failure can only abandon the stream. That is deliberate — the archive
// then lacks its index, so the client sees a broken download rather than a file
// that looks complete and is not.
func (m *Manager) WriteArchive(ctx context.Context, user *model.User, ids []uint, w io.Writer, format string, level int, onProgress ArchiveProgress) error {
	if len(ids) == 0 {
		return errors.New("nothing to archive")
	}
	format, err := m.ArchiveFormat(format)
	if err != nil {
		return err
	}
	aw, err := archive.NewWriter(w, format, m.ArchiveLevel(level))
	if err != nil {
		return err
	}
	var entries int
	var written int64
	err = m.walkTree(ctx, user, ids, func(e treeEntry) error {
		// Checked per member as well as inside the read, so a cancelled job
		// stops at the next boundary at the latest.
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.isDir {
			return aw.AddDir(e.name)
		}
		rc, err := m.openContent(ctx, e.file)
		if err != nil {
			return fmt.Errorf("archive %q: %w", e.name, err)
		}
		defer rc.Close()
		if err := aw.AddFile(e.name, e.file.Size, e.file.UpdatedAt, &ctxReader{ctx: ctx, r: rc}); err != nil {
			return err
		}
		entries++
		written += e.file.Size
		if onProgress != nil {
			onProgress(entries, written)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return aw.Close()
}
