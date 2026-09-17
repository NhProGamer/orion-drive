package filemanager

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
)

// maxTreeDepth bounds how deep an archive walk follows folders. The drive's
// tree cannot legitimately nest this far, so hitting it means a parent cycle
// left by a bug — which would otherwise recurse forever.
const maxTreeDepth = 64

// ErrArchiveTooDeep is returned when a tree nests past maxTreeDepth.
var ErrArchiveTooDeep = errors.New("folder tree is nested too deeply to archive")

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

// WriteArchive streams a ZIP of the given files/folders (recursively) to w.
//
// Callers are expected to have run PlanArchive first: once w has been written
// to, a failure can only abandon the stream. That is deliberate — the archive
// then lacks its central directory, so the client sees a broken download
// rather than a file that looks complete and is not.
func (m *Manager) WriteArchive(ctx context.Context, user *model.User, ids []uint, w io.Writer) error {
	if len(ids) == 0 {
		return errors.New("nothing to archive")
	}
	zw := zip.NewWriter(w)
	err := m.walkTree(ctx, user, ids, func(e treeEntry) error {
		if e.isDir {
			_, err := zw.Create(e.name + "/")
			return err
		}
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: e.file.UpdatedAt}
		// Without a mode, entries unpack with no permissions at all on some
		// extractors.
		hdr.SetMode(0o644)
		wr, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		rc, err := m.openContent(ctx, e.file)
		if err != nil {
			return fmt.Errorf("archive %q: %w", e.name, err)
		}
		defer rc.Close()
		_, err = io.Copy(wr, rc)
		return err
	})
	if err != nil {
		return err
	}
	return zw.Close()
}
