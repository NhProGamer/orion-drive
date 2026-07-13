package filemanager

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/archive"
	"github.com/NhProGamer/orion-drive/pkg/queue"
)

// Compress schedules a background job that zips the given files/folders and
// stores the archive as a new file under parentID.
func (m *Manager) Compress(ctx context.Context, user *model.User, parentID *uint, ids []uint, name string) (*queue.Job, error) {
	if len(ids) == 0 {
		return nil, errors.New("nothing to compress")
	}
	if strings.TrimSpace(name) == "" {
		name = "archive.zip"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".zip") {
		name += ".zip"
	}
	if err := m.ensureParent(ctx, user, parentID); err != nil {
		return nil, err
	}

	job := m.queue.Enqueue(user.ID, "compress", func(ctx context.Context, report queue.Report) (map[string]any, error) {
		report(10, "Création de l’archive")
		tmp, err := os.CreateTemp(m.tmpDir, "compress-*.zip")
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()

		if err := m.WriteArchive(ctx, user, ids, tmp); err != nil {
			return nil, err
		}
		size, err := tmp.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, err
		}
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}

		report(70, "Enregistrement")
		f, err := m.ingestContent(ctx, user, parentID, name, tmp, size)
		if err != nil {
			return nil, err
		}
		return map[string]any{"file_id": f.ID, "name": f.Name, "size": size}, nil
	})
	return job, nil
}

// Extract schedules a background job that unpacks an archive file into destParentID.
func (m *Manager) Extract(ctx context.Context, user *model.User, fileID uint, destParentID *uint) (*queue.Job, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, fileID)
	if err != nil {
		return nil, err
	}
	if f.IsFolder() || !archive.IsArchive(f.Name) {
		return nil, errors.New("not a supported archive")
	}
	if err := m.ensureParent(ctx, user, destParentID); err != nil {
		return nil, err
	}

	job := m.queue.Enqueue(user.ID, "extract", func(ctx context.Context, report queue.Report) (map[string]any, error) {
		report(10, "Lecture de l’archive")
		tmpPath, err := m.bufferContent(ctx, f)
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmpPath)

		report(30, "Extraction")
		count := 0
		err = archive.Extract(tmpPath, func(e archive.Entry, open func() (io.ReadCloser, error)) error {
			clean := strings.Trim(path.Clean("/"+e.Name), "/")
			if clean == "" {
				return nil
			}
			if e.IsDir {
				_, err := m.mkdirs(ctx, user, destParentID, clean)
				return err
			}
			dir, base := path.Split(clean)
			parent := destParentID
			if d := strings.Trim(dir, "/"); d != "" {
				p, err := m.mkdirs(ctx, user, destParentID, d)
				if err != nil {
					return err
				}
				parent = p
			}
			rc, err := open()
			if err != nil {
				return err
			}
			defer rc.Close()
			if _, err := m.ingestContent(ctx, user, parent, base, rc, e.Size); err != nil {
				return err
			}
			count++
			report(-1, fmt.Sprintf("%d fichier(s) extrait(s)", count))
			return nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"count": count}, nil
	})
	return job, nil
}

// ListArchiveEntries returns the entries of an archive file without extracting it.
func (m *Manager) ListArchiveEntries(ctx context.Context, user *model.User, fileID uint) ([]archive.Entry, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, fileID)
	if err != nil {
		return nil, err
	}
	if f.IsFolder() || !archive.IsArchive(f.Name) {
		return nil, errors.New("not a supported archive")
	}
	tmpPath, err := m.bufferContent(ctx, f)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpPath)
	return archive.List(tmpPath)
}

// bufferContent writes a file's (decrypted) content to a temp file whose name
// preserves the archive extension, and returns its path. The caller removes it.
func (m *Manager) bufferContent(ctx context.Context, f *model.File) (string, error) {
	if err := os.MkdirAll(m.tmpDir, 0o755); err != nil {
		return "", err
	}
	rc, err := m.openContent(ctx, f)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	tmp, err := os.CreateTemp(m.tmpDir, "archive-*-"+sanitize(f.Name))
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, rc); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// ingestContent stores r as a new file named uniquely under parentID, encrypting
// at rest when the policy requires it, and updates the user's used storage.
func (m *Manager) ingestContent(ctx context.Context, user *model.User, parentID *uint, name string, r io.Reader, size int64) (*model.File, error) {
	name = strings.TrimSpace(name)
	if err := validateName(name); err != nil {
		return nil, err
	}
	policy, err := m.policyForUser(ctx, user)
	if err != nil {
		return nil, err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return nil, err
	}

	var props model.JSON
	if m.policyEncrypts(policy) {
		if m.cipher == nil {
			return nil, errors.New("policy requests encryption but no encryption key is configured")
		}
		if !h.Capabilities().LocalServe {
			return nil, errors.New("encryption is only supported on local-serve storage policies")
		}
		enc, iv, err := m.cipher.EncryptReader(r)
		if err != nil {
			return nil, err
		}
		r = enc
		props = model.MustJSON(model.EntityProps{IV: base64.StdEncoding.EncodeToString(iv)})
	}

	source := newSourcePath(user.ID, name)
	if err := h.Put(ctx, source, r, size); err != nil {
		return nil, err
	}

	entity := &model.Entity{
		Type:            model.EntityTypeVersion,
		Source:          source,
		Size:            size,
		ReferenceCount:  1,
		StoragePolicyID: policy.ID,
		CreatedByID:     user.ID,
		Props:           props,
	}
	if err := m.repo.Entity.Create(ctx, entity); err != nil {
		return nil, err
	}

	file := &model.File{
		Name:            m.uniqueName(ctx, user, parentID, name),
		Type:            model.FileTypeFile,
		OwnerID:         user.ID,
		ParentID:        parentID,
		PrimaryEntityID: &entity.ID,
		Size:            size,
		StoragePolicyID: policy.ID,
	}
	if err := m.repo.File.Create(ctx, file); err != nil {
		return nil, err
	}
	entity.FileID = &file.ID
	if err := m.repo.Entity.Update(ctx, entity); err != nil {
		return nil, err
	}

	user.StorageUsed += size
	_ = m.repo.User.Update(ctx, user)
	return file, nil
}

// mkdirs resolves (creating as needed) a slash-separated directory path under
// root and returns the id of the deepest folder.
func (m *Manager) mkdirs(ctx context.Context, user *model.User, root *uint, dirPath string) (*uint, error) {
	parent := root
	for _, part := range strings.Split(dirPath, "/") {
		if part == "" || part == "." {
			continue
		}
		id, err := m.getOrCreateFolder(ctx, user, parent, part)
		if err != nil {
			return nil, err
		}
		parent = id
	}
	return parent, nil
}

// getOrCreateFolder returns the id of a child folder named name under parentID,
// creating it if missing.
func (m *Manager) getOrCreateFolder(ctx context.Context, user *model.User, parentID *uint, name string) (*uint, error) {
	if existing, err := m.repo.File.FindChildByName(ctx, user.ID, parentID, name); err == nil {
		if existing.IsFolder() {
			id := existing.ID
			return &id, nil
		}
	}
	f := &model.File{
		Name:            name,
		Type:            model.FileTypeFolder,
		OwnerID:         user.ID,
		ParentID:        parentID,
		StoragePolicyID: 0,
	}
	if err := m.repo.File.Create(ctx, f); err != nil {
		return nil, err
	}
	id := f.ID
	return &id, nil
}

// uniqueName appends " (n)" until the name is free under parentID.
func (m *Manager) uniqueName(ctx context.Context, user *model.User, parentID *uint, name string) string {
	if _, err := m.repo.File.FindChildByName(ctx, user.ID, parentID, name); err != nil {
		return name
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 10000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := m.repo.File.FindChildByName(ctx, user.ID, parentID, candidate); err != nil {
			return candidate
		}
	}
	return name
}

// sanitize keeps a file name safe for use as a temp-file suffix.
func sanitize(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == os.PathSeparator {
			return '_'
		}
		return r
	}, path.Base(name))
}
