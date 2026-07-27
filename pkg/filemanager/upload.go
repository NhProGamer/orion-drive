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
	"strconv"
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
	// FileID is set when the upload replaces an existing file, adding a new
	// version to it instead of creating a new file.
	FileID *uint `json:"file_id,omitempty"`
	// ShareToken, when set, marks a session created through a write/deposit share
	// link by an anonymous visitor. Contributor is the optional name they gave.
	ShareToken  string `json:"share_token,omitempty"`
	Contributor string `json:"contributor,omitempty"`
}

// NumChunks returns how many chunks the upload is split into.
func (s *UploadSession) NumChunks() int {
	if s.Size == 0 {
		return 0
	}
	return int((s.Size + s.ChunkSize - 1) / s.ChunkSize)
}

// Complete reports whether every chunk has been received.
func uploadKey(id string) string { return "upload:" + id }

// chunkKey marks one received chunk. Each chunk sets its own key (an atomic
// single-key write) so concurrent PutChunk calls don't lose each other's
// progress — the read-modify-write of a shared Received slice would race.
func chunkKey(id string, index int) string { return "upload:" + id + ":c:" + strconv.Itoa(index) }

// receivedAll reports whether every chunk of an upload has been recorded.
func (m *Manager) receivedAll(id string, num int) bool {
	for i := 0; i < num; i++ {
		if _, ok := m.cache.Get(chunkKey(id, i)); !ok {
			return false
		}
	}
	return true
}

// UploadProgress reports, for an in-flight upload of num chunks, which chunk
// indices have been received (read from the per-chunk cache markers) and whether
// the upload is complete. Backs the upload status responses; the real completion
// gate at finalisation is receivedAll (which short-circuits).
func (m *Manager) UploadProgress(id string, num int) (received []bool, complete bool) {
	received = make([]bool, num)
	complete = true
	for i := 0; i < num; i++ {
		if _, ok := m.cache.Get(chunkKey(id, i)); ok {
			received[i] = true
		} else {
			complete = false
		}
	}
	return received, complete
}

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

// AttachShare tags a session as belonging to a share link and persists it. Used
// right after InitUpload when the upload is driven by an anonymous share visitor.
func (m *Manager) AttachShare(s *UploadSession, token, contributor string) error {
	s.ShareToken = token
	s.Contributor = contributor
	return m.saveSession(s)
}

// GetShareSession loads a session created through a share link, authorized by
// the share token rather than a user id (the visitor is anonymous). It returns
// ErrNotFound if the session is unknown or was not created for this token.
func (m *Manager) GetShareSession(token, id string) (*UploadSession, error) {
	raw, ok := m.cache.Get(uploadKey(id))
	if !ok {
		return nil, ErrNotFound
	}
	var s UploadSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s.ShareToken == "" || s.ShareToken != token {
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
	if index < 0 || index >= s.NumChunks() {
		return nil, fmt.Errorf("chunk index %d out of range", index)
	}

	f, err := os.OpenFile(s.TempPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	offset := int64(index) * s.ChunkSize
	sw := &sectionWriter{f: f, off: offset}
	// Cap the chunk to the negotiated size. Without this a client can stream an
	// unbounded body into the staging file and exhaust the disk before the total
	// is ever checked (the declared total is quota-checked at InitUpload, and the
	// number of chunks is bounded, so per-chunk capping bounds on-disk bytes).
	n, err := io.Copy(sw, io.LimitReader(r, s.ChunkSize))
	if err != nil {
		return nil, err
	}
	if n == s.ChunkSize {
		var probe [1]byte
		if extra, _ := io.ReadFull(r, probe[:]); extra > 0 {
			return nil, fmt.Errorf("chunk %d exceeds the negotiated chunk size", index)
		}
	}

	// Record this chunk with its own cache key (concurrency-safe; see chunkKey).
	if err := m.cache.Set(chunkKey(id, index), []byte{1}, uploadSessionTTL); err != nil {
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
	if !m.receivedAll(s.ID, s.NumChunks()) {
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

	// Stream the staged file (or an empty reader) into the backend.
	var reader io.Reader = strings.NewReader("")
	if s.Size > 0 {
		tmp, err := os.Open(s.TempPath)
		if err != nil {
			return nil, err
		}
		defer tmp.Close()
		reader = io.LimitReader(tmp, s.Size)
	}

	file, err := m.commitContent(ctx, user, s.ParentID, s.Name, s.FileID, policy, h, reader, s.Size)
	if err != nil {
		return nil, err
	}

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
	for i, n := 0, s.NumChunks(); i < n; i++ {
		_ = m.cache.Delete(chunkKey(s.ID, i))
	}
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
