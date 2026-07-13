package filemanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/model"
)

// DefaultChunkSize is the upload chunk size negotiated with clients (8 MiB).
const DefaultChunkSize int64 = 8 << 20

const uploadSessionTTL = 24 * time.Hour

// UploadSession is the server-side state of an in-progress resumable upload.
// It is stored in the cache (JSON) and keyed by ID.
type UploadSession struct {
	ID        string `json:"id"`
	UserID    uint   `json:"user_id"`
	ParentID  *uint  `json:"parent_id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	ChunkSize int64  `json:"chunk_size"`
	PolicyID  uint   `json:"policy_id"`
	TempPath  string `json:"temp_path"`
	Received  []bool `json:"received"`
	// FileID is set when the upload replaces an existing file, adding a new
	// version to it instead of creating a new file.
	FileID *uint `json:"file_id,omitempty"`
}

// NumChunks returns how many chunks the upload is split into.
func (s *UploadSession) NumChunks() int {
	if s.Size == 0 {
		return 0
	}
	return int((s.Size + s.ChunkSize - 1) / s.ChunkSize)
}

// Complete reports whether every chunk has been received.
func (s *UploadSession) Complete() bool {
	for _, r := range s.Received {
		if !r {
			return false
		}
	}
	return true
}

func uploadKey(id string) string { return "upload:" + id }

// InitUpload validates the request, reserves a session and returns it.
func (m *Manager) InitUpload(ctx context.Context, user *model.User, parentID *uint, name string, size int64) (*UploadSession, error) {
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

	// If a file with this name already exists, the upload becomes a new version
	// of it (folders still conflict).
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

	// Quota check.
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

	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	id := hex.EncodeToString(buf)

	if err := os.MkdirAll(m.tmpDir, 0o755); err != nil {
		return nil, err
	}
	sess := &UploadSession{
		ID:        id,
		UserID:    user.ID,
		ParentID:  parentID,
		Name:      name,
		Size:      size,
		ChunkSize: DefaultChunkSize,
		PolicyID:  policy.ID,
		TempPath:  filepath.Join(m.tmpDir, id+".part"),
		FileID:    targetFileID,
	}
	sess.Received = make([]bool, sess.NumChunks())
	if err := m.saveSession(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// GetSession loads a session owned by the user (for resume/status).
func (m *Manager) GetSession(user *model.User, id string) (*UploadSession, error) {
	raw, ok := m.cache.Get(uploadKey(id))
	if !ok {
		return nil, ErrNotFound
	}
	var s UploadSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s.UserID != user.ID {
		return nil, ErrNotFound
	}
	return &s, nil
}

func (m *Manager) saveSession(s *UploadSession) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return m.cache.Set(uploadKey(s.ID), raw, uploadSessionTTL)
}

// PutChunk writes one chunk at its offset and records it as received.
func (m *Manager) PutChunk(ctx context.Context, user *model.User, id string, index int, r io.Reader) (*UploadSession, error) {
	s, err := m.GetSession(user, id)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(s.Received) {
		return nil, fmt.Errorf("chunk index %d out of range", index)
	}

	f, err := os.OpenFile(s.TempPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	offset := int64(index) * s.ChunkSize
	sw := &sectionWriter{f: f, off: offset}
	if _, err := io.Copy(sw, r); err != nil {
		return nil, err
	}

	s.Received[index] = true
	if err := m.saveSession(s); err != nil {
		return nil, err
	}
	return s, nil
}

// CompleteUpload finalizes the upload: it flushes the staged file into storage,
// creates the Entity and File rows, and updates the user's used storage.
func (m *Manager) CompleteUpload(ctx context.Context, user *model.User, id string) (*model.File, error) {
	s, err := m.GetSession(user, id)
	if err != nil {
		return nil, err
	}
	if !s.Complete() {
		return nil, errors.New("upload incomplete")
	}

	policy, err := m.repo.Policy.GetByID(ctx, s.PolicyID)
	if err != nil {
		return nil, err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return nil, err
	}

	source := newSourcePath(user.ID, s.Name)

	// Stream the staged file (or an empty reader) into the backend.
	var reader io.Reader = strings.NewReader("")
	var tmp *os.File
	if s.Size > 0 {
		tmp, err = os.Open(s.TempPath)
		if err != nil {
			return nil, err
		}
		defer tmp.Close()
		reader = io.LimitReader(tmp, s.Size)
	}
	if err := h.Put(ctx, source, reader, s.Size); err != nil {
		return nil, err
	}

	entity := &model.Entity{
		Type:            model.EntityTypeVersion,
		Source:          source,
		Size:            s.Size,
		ReferenceCount:  1,
		StoragePolicyID: policy.ID,
		CreatedByID:     user.ID,
	}
	if err := m.repo.Entity.Create(ctx, entity); err != nil {
		return nil, err
	}

	var file *model.File
	if s.FileID != nil {
		// Add a new version to the existing file; the old versions are kept.
		file, err = m.repo.File.GetByID(ctx, user.ID, *s.FileID)
		if err != nil {
			return nil, err
		}
		entity.FileID = &file.ID
		if err := m.repo.Entity.Update(ctx, entity); err != nil {
			return nil, err
		}
		file.PrimaryEntityID = &entity.ID
		file.Size = s.Size
		file.StoragePolicyID = policy.ID
		if err := m.repo.File.Update(ctx, file); err != nil {
			return nil, err
		}
	} else {
		file = &model.File{
			Name:            s.Name,
			Type:            model.FileTypeFile,
			OwnerID:         user.ID,
			ParentID:        s.ParentID,
			PrimaryEntityID: &entity.ID,
			Size:            s.Size,
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

	user.StorageUsed += s.Size
	_ = m.repo.User.Update(ctx, user)

	m.discardSession(s)
	return file, nil
}

// CancelUpload aborts and cleans up an in-progress upload.
func (m *Manager) CancelUpload(user *model.User, id string) error {
	s, err := m.GetSession(user, id)
	if err != nil {
		return err
	}
	m.discardSession(s)
	return nil
}

func (m *Manager) discardSession(s *UploadSession) {
	_ = os.Remove(s.TempPath)
	_ = m.cache.Delete(uploadKey(s.ID))
}

// sectionWriter writes sequentially starting at a fixed offset using WriteAt.
type sectionWriter struct {
	f   *os.File
	off int64
}

func (w *sectionWriter) Write(p []byte) (int, error) {
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	return n, err
}
