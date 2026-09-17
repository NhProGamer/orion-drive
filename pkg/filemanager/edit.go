package filemanager

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"

	"github.com/NhProGamer/orion-drive/model"
)

// CreateFile makes a new file holding content under parentID.
//
// Unlike WriteFile it never writes into a file that is already there: a name
// already taken gets the usual "name (2)" suffix. That is the difference
// between creating a document and uploading one — a mistyped new document must
// not blank the document of the same name sitting next to it.
func (m *Manager) CreateFile(ctx context.Context, user *model.User, parentID *uint, name string, content []byte) (*model.File, error) {
	name = strings.TrimSpace(name)
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := m.ensureParent(ctx, user, parentID); err != nil {
		return nil, err
	}
	unique := m.uniqueName(ctx, user, parentID, name)
	return m.WriteFile(ctx, user, parentID, unique, bytes.NewReader(content), int64(len(content)))
}

// SaveVersion overwrites a file's content, keeping the previous content as a
// version. Used by the in-browser text/Markdown editor.
func (m *Manager) SaveVersion(ctx context.Context, user *model.User, fileID uint, content []byte) (*model.File, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, fileID)
	if err != nil {
		return nil, err
	}
	if f.IsFolder() {
		return nil, errors.New("cannot edit a folder")
	}
	if f.IsLocked() {
		return nil, ErrLocked
	}
	if err := m.storeVersion(ctx, user, f, bytes.NewReader(content), int64(len(content))); err != nil {
		return nil, err
	}
	return f, nil
}

// OpenFileContent returns a plain (decrypted) reader for a user's file, plus the
// file record. Callers must Close the reader. Used by the WOPI host.
func (m *Manager) OpenFileContent(ctx context.Context, user *model.User, fileID uint) (io.ReadCloser, *model.File, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, fileID)
	if err != nil {
		return nil, nil, err
	}
	if f.IsFolder() {
		return nil, nil, errors.New("not a file")
	}
	rc, err := m.openContent(ctx, f)
	if err != nil {
		return nil, nil, err
	}
	return rc, f, nil
}

// storeVersion writes r as a new primary version of file, encrypting at rest
// when the policy requires it, and adjusts the user's used storage.
func (m *Manager) storeVersion(ctx context.Context, user *model.User, file *model.File, r io.Reader, size int64) error {
	policy, err := m.policyForUser(ctx, user)
	if err != nil {
		return err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return err
	}

	var props model.JSON
	if m.policyEncrypts(policy) {
		if m.cipher == nil {
			return errors.New("policy requests encryption but no encryption key is configured")
		}
		if !h.Capabilities().LocalServe {
			return errors.New("encryption is only supported on local-serve storage policies")
		}
		enc, iv, err := m.cipher.EncryptReader(r)
		if err != nil {
			return err
		}
		r = enc
		props = model.MustJSON(model.EntityProps{IV: base64.StdEncoding.EncodeToString(iv)})
	}

	source := newSourcePath(user.ID, file.Name)
	if err := h.Put(ctx, source, r, size); err != nil {
		return err
	}

	entity := &model.Entity{
		Type:            model.EntityTypeVersion,
		Source:          source,
		Size:            size,
		ReferenceCount:  1,
		StoragePolicyID: policy.ID,
		CreatedByID:     user.ID,
		FileID:          &file.ID,
		Props:           props,
	}
	if err := m.repo.Entity.Create(ctx, entity); err != nil {
		return err
	}

	file.PrimaryEntityID = &entity.ID
	file.Size = size
	file.StoragePolicyID = policy.ID
	if err := m.repo.File.Update(ctx, file); err != nil {
		return err
	}

	m.addStorage(ctx, user, size)
	m.pruneVersions(ctx, user, file)
	return nil
}
