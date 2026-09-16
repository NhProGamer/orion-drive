package filemanager

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"

	"github.com/NhProGamer/orion-drive/model"
)

// SnapshotStore exposes the manager as the backing store of the live-editing
// relays: it loads a document and writes snapshots back as the file's owner,
// whoever is actually editing. Used by both the whiteboard and the
// collaborative Markdown editor.
type SnapshotStore struct{ m *Manager }

// Snapshots returns the live-editing snapshot store backed by this manager.
func (m *Manager) Snapshots() *SnapshotStore { return &SnapshotStore{m: m} }

// Load returns the file's stored content. A file with no content yet (one that
// was just created) yields an empty slice rather than an error.
func (s *SnapshotStore) Load(ctx context.Context, ownerID, fileID uint) ([]byte, error) {
	owner, err := s.m.repo.User.GetByID(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	f, err := s.m.repo.File.GetByID(ctx, owner.ID, fileID)
	if err != nil {
		return nil, err
	}
	if f.IsFolder() {
		return nil, errors.New("not a file")
	}
	if f.PrimaryEntityID == nil {
		return nil, nil
	}
	rc, err := s.m.openContent(ctx, f)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// Save writes content as the file's content. entityID is the version a previous
// save in the same session produced: it is overwritten in place so a long
// editing session leaves a single entry in the file's history instead of one per
// autosave. Passing 0 (the first save of a session) appends a new version, which
// is what makes the state the session started from recoverable.
func (s *SnapshotStore) Save(ctx context.Context, ownerID, fileID, entityID uint, content []byte) (uint, error) {
	owner, err := s.m.repo.User.GetByID(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	f, err := s.m.repo.File.GetByID(ctx, owner.ID, fileID)
	if err != nil {
		return 0, err
	}
	if f.IsFolder() {
		return 0, errors.New("cannot save a folder")
	}
	if f.IsLocked() {
		return 0, ErrLocked
	}
	if entityID != 0 {
		if ok, err := s.overwrite(ctx, owner, f, entityID, content); err != nil {
			return 0, err
		} else if ok {
			return entityID, nil
		}
		// The version is no longer overwritable (restored, pruned, or its stored
		// object became shared): fall through and append a new one.
	}
	if err := s.m.storeVersion(ctx, owner, f, bytes.NewReader(content), int64(len(content))); err != nil {
		return 0, err
	}
	if f.PrimaryEntityID == nil {
		return 0, errors.New("version was not committed")
	}
	return *f.PrimaryEntityID, nil
}

// overwrite replaces the content of the session's own version in place. It
// reports false (with no error) when that version is not the file's current one
// or its stored object is shared with another entity through deduplication —
// cases where rewriting it would corrupt unrelated content, and the caller must
// append a new version instead.
func (s *SnapshotStore) overwrite(ctx context.Context, owner *model.User, f *model.File, entityID uint, content []byte) (bool, error) {
	if f.PrimaryEntityID == nil || *f.PrimaryEntityID != entityID {
		return false, nil
	}
	e, err := s.m.repo.Entity.GetByID(ctx, entityID)
	if err != nil {
		return false, nil
	}
	if e.FileID == nil || *e.FileID != f.ID || e.Type != model.EntityTypeVersion {
		return false, nil
	}
	if n, err := s.m.repo.Entity.CountBySource(ctx, e.Source, entityID); err != nil || n > 0 {
		return false, nil
	}

	policy, err := s.m.repo.Policy.GetByID(ctx, e.StoragePolicyID)
	if err != nil {
		return false, err
	}
	h, err := s.m.driverForPolicy(policy)
	if err != nil {
		return false, err
	}

	var (
		reader io.Reader = bytes.NewReader(content)
		size             = int64(len(content))
		props            = e.Props
	)
	if s.m.policyEncrypts(policy) {
		if s.m.cipher == nil {
			return false, errors.New("policy requests encryption but no encryption key is configured")
		}
		// A rewritten object gets a fresh IV: reusing the previous one with a
		// different plaintext under the same key leaks the difference of the two
		// documents to anyone holding both ciphertexts.
		enc, iv, err := s.m.cipher.EncryptReader(reader)
		if err != nil {
			return false, err
		}
		reader = enc
		props = model.MustJSON(model.EntityProps{IV: base64.StdEncoding.EncodeToString(iv)})
	}
	if err := h.Put(ctx, e.Source, reader, size); err != nil {
		return false, err
	}

	delta := size - e.Size
	e.Size = size
	e.Props = props
	if err := s.m.repo.Entity.Update(ctx, e); err != nil {
		return false, err
	}
	f.Size = size
	if err := s.m.repo.File.Update(ctx, f); err != nil {
		return false, err
	}
	if delta != 0 {
		s.m.addStorage(ctx, owner, delta)
	}
	return true, nil
}
