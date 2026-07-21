package filemanager

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/thumb"
)

// ErrNoThumbnail is returned when a file cannot have a thumbnail.
var ErrNoThumbnail = errors.New("no thumbnail available")

// maxThumbSource caps how many bytes are read to build an image thumbnail.
const maxThumbSource = 100 << 20

// Thumbnail returns a JPEG thumbnail for a file, generating and caching it on
// first request. Thumbnails are stored as a separate (thumb) entity and are
// regenerated when the file's content changes.
func (m *Manager) Thumbnail(ctx context.Context, user *model.User, id uint) ([]byte, error) {
	f, err := m.repo.File.GetByID(ctx, user.ID, id)
	if err != nil {
		return nil, err
	}
	if f.IsFolder() || f.PrimaryEntityID == nil {
		return nil, ErrNoThumbnail
	}
	kind := thumb.Kind(path.Ext(f.Name))
	if kind == "" || !thumb.Available(kind) {
		return nil, ErrNoThumbnail
	}

	// Serve a cached thumbnail if it matches the current version.
	if e, err := m.repo.Entity.GetThumb(ctx, f.ID); err == nil {
		if e.DecodeProps().ThumbOf == *f.PrimaryEntityID {
			return m.readEntityBytes(ctx, e)
		}
		m.removeEntity(ctx, e.ID) // stale — regenerate below
	}

	// Collapse concurrent requests for the same file+version into one generation
	// (thundering-herd guard). The winner re-checks the cache — another request
	// may have just produced it — then generates and stores.
	key := fmt.Sprintf("thumb:%d:%d", f.ID, *f.PrimaryEntityID)
	v, err, _ := m.thumbSF.Do(key, func() (any, error) {
		if e, err := m.repo.Entity.GetThumb(ctx, f.ID); err == nil && e.DecodeProps().ThumbOf == *f.PrimaryEntityID {
			return m.readEntityBytes(ctx, e)
		}
		data, gerr := m.generateThumb(ctx, f, kind)
		if gerr != nil {
			return nil, gerr
		}
		// Caching failures are non-fatal: still return the freshly generated image.
		_ = m.storeThumb(ctx, user, f, data)
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// generateThumb builds thumbnail bytes for a file.
func (m *Manager) generateThumb(ctx context.Context, f *model.File, kind string) ([]byte, error) {
	// Thumbnailing is memory/CPU heavy — an in-process image decode (a crafted
	// image can be a decompression bomb) or an external generator. Cap how many
	// run concurrently across ALL kinds so N crafted inputs can't exhaust the
	// host (the image path previously bypassed this guard).
	select {
	case m.thumbSem <- struct{}{}:
		defer func() { <-m.thumbSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	switch kind {
	case thumb.KindImage:
		rc, err := m.openContent(ctx, f)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, maxThumbSource))
		if err != nil {
			return nil, err
		}
		return thumb.Image(data)
	case thumb.KindVideo, thumb.KindAudio, thumb.KindVIPS, thumb.KindRaw, thumb.KindDocument, thumb.KindPDF:
		// These generators work on a file path; buffer the (decrypted) content.
		p, err := m.bufferContent(ctx, f)
		if err != nil {
			return nil, err
		}
		defer os.Remove(p)
		switch kind {
		case thumb.KindVideo:
			return thumb.Video(ctx, p)
		case thumb.KindAudio:
			return thumb.Audio(ctx, p)
		case thumb.KindVIPS:
			return thumb.VIPS(ctx, p)
		case thumb.KindRaw:
			return thumb.Raw(ctx, p)
		case thumb.KindDocument:
			return thumb.Document(ctx, p)
		case thumb.KindPDF:
			return thumb.PDF(ctx, p)
		}
	}
	return nil, ErrNoThumbnail
}

// storeThumb persists thumbnail bytes as a thumb entity (encrypt-aware on
// local-serve policies). Thumbnails do not count against the user's quota.
func (m *Manager) storeThumb(ctx context.Context, user *model.User, f *model.File, data []byte) error {
	policy, err := m.policyForUser(ctx, user)
	if err != nil {
		return err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return err
	}

	props := model.EntityProps{ThumbOf: *f.PrimaryEntityID}
	var reader io.Reader = bytes.NewReader(data)
	if m.policyEncrypts(policy) && m.cipher != nil && h.Capabilities().LocalServe {
		enc, iv, err := m.cipher.EncryptReader(reader)
		if err != nil {
			return err
		}
		reader = enc
		props.IV = base64.StdEncoding.EncodeToString(iv)
	}

	source := newSourcePath(user.ID, f.Name+".thumb.jpg")
	if err := h.Put(ctx, source, reader, int64(len(data))); err != nil {
		return err
	}
	e := &model.Entity{
		Type:            model.EntityTypeThumb,
		Source:          source,
		Size:            int64(len(data)),
		ReferenceCount:  1,
		StoragePolicyID: policy.ID,
		CreatedByID:     user.ID,
		FileID:          &f.ID,
		Props:           model.MustJSON(props),
	}
	return m.repo.Entity.Create(ctx, e)
}

// readEntityBytes reads an entity's (decrypted) content fully into memory.
func (m *Manager) readEntityBytes(ctx context.Context, e *model.Entity) ([]byte, error) {
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
	defer rc.Close()

	var reader io.Reader = rc
	if e.Encrypted() {
		if m.cipher == nil {
			return nil, errors.New("cannot decrypt thumbnail: no encryption key")
		}
		iv, err := base64.StdEncoding.DecodeString(e.DecodeProps().IV)
		if err != nil {
			return nil, err
		}
		dec, err := m.cipher.DecryptReadSeeker(rc, iv)
		if err != nil {
			return nil, err
		}
		reader = dec
	}
	return io.ReadAll(reader)
}
