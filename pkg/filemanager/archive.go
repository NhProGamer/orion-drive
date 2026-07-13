package filemanager

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"path"

	"github.com/NhProGamer/orion-drive/model"
)

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

// WriteArchive streams a ZIP of the given files/folders (recursively) to w.
func (m *Manager) WriteArchive(ctx context.Context, user *model.User, ids []uint, w io.Writer) error {
	if len(ids) == 0 {
		return errors.New("nothing to archive")
	}
	zw := zip.NewWriter(w)
	for _, id := range ids {
		f, err := m.repo.File.GetByID(ctx, user.ID, id)
		if err != nil {
			return err
		}
		if err := m.addToZip(ctx, zw, f, ""); err != nil {
			return err
		}
	}
	return zw.Close()
}

// addToZip writes a file (or a folder subtree) into the archive under prefix.
func (m *Manager) addToZip(ctx context.Context, zw *zip.Writer, f *model.File, prefix string) error {
	name := path.Join(prefix, f.Name)
	if f.IsFolder() {
		children, err := m.repo.File.ListChildren(ctx, f.OwnerID, &f.ID)
		if err != nil {
			return err
		}
		if len(children) == 0 {
			_, err := zw.Create(name + "/")
			return err
		}
		for i := range children {
			if err := m.addToZip(ctx, zw, &children[i], name); err != nil {
				return err
			}
		}
		return nil
	}

	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: f.UpdatedAt}
	wr, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	rc, err := m.openContent(ctx, f)
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(wr, rc)
	return err
}
