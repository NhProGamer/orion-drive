package filemanager

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/archive"
	"github.com/NhProGamer/orion-drive/pkg/queue"
)

// ArchiveLimits are the safety limits applied when extracting archives (defence
// against decompression bombs and quota abuse). Zero fields fall back to the
// defaults below.
type ArchiveLimits struct {
	// MaxEntries caps how many members an archive may unpack to, guarding against
	// archives with millions of tiny entries.
	MaxEntries int
	// MaxUncompressed bounds an extraction when the user has no storage quota, so
	// a decompression bomb still cannot fill the disk.
	MaxUncompressed int64
	// MaxRatio rejects an archive whose uncompressed size exceeds this many times
	// its compressed size — an egregious decompression bomb — but only above
	// RatioFloor, so ordinary highly-compressible files are not flagged.
	MaxRatio   int64
	RatioFloor int64
	// Timeout caps how long an extraction may run, bounding CPU on a bomb that is
	// cheap to read but expensive to decompress (e.g. a gzip bomb).
	Timeout time.Duration
}

// Default archive-limit values, used for any field left zero.
const (
	defArchiveMaxEntries      = 100_000
	defArchiveMaxUncompressed = int64(50 << 30) // 50 GiB
	defArchiveMaxRatio        = int64(1000)
	defArchiveRatioFloor      = int64(1 << 30) // 1 GiB
	defArchiveTimeout         = 5 * time.Minute
)

// withDefaults returns the limits with any zero field replaced by its default.
func (l ArchiveLimits) withDefaults() ArchiveLimits {
	if l.MaxEntries <= 0 {
		l.MaxEntries = defArchiveMaxEntries
	}
	if l.MaxUncompressed <= 0 {
		l.MaxUncompressed = defArchiveMaxUncompressed
	}
	if l.MaxRatio <= 0 {
		l.MaxRatio = defArchiveMaxRatio
	}
	if l.RatioFloor <= 0 {
		l.RatioFloor = defArchiveRatioFloor
	}
	if l.Timeout <= 0 {
		l.Timeout = defArchiveTimeout
	}
	return l
}

// ErrArchiveTooLarge is returned when extracting an archive would exceed the
// user's storage quota, hit the unlimited-quota safety cap, or when an entry's
// decompressed stream runs past its declared size (a zip bomb).
var ErrArchiveTooLarge = errors.New("archive is too large to extract (quota exceeded or decompression bomb)")

// ErrTooManyFiles is returned when an archive holds more entries than allowed.
var ErrTooManyFiles = errors.New("archive has too many entries to extract")

// boundedReader fails once more than max bytes have been read, so a lying entry
// header whose decompressed stream keeps producing data cannot overrun.
type boundedReader struct {
	r   io.Reader
	n   int64
	max int64
}

func (b *boundedReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.n += int64(n)
	if b.n > b.max {
		return n, ErrArchiveTooLarge
	}
	return n, err
}

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

	// Measured before the job is even scheduled, so an impossible request is
	// refused while the caller is still listening.
	if _, err := m.PlanArchive(ctx, user, ids); err != nil {
		return nil, err
	}

	lim := m.archive
	job := m.queue.Enqueue(user.ID, "compress", func(ctx context.Context, report queue.Report) (map[string]any, error) {
		// Bound compression time too: zipping a very large tree should not run
		// unbounded.
		ctx, cancel := context.WithTimeout(ctx, lim.Timeout)
		defer cancel()

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
		// The produced archive counts against the user's quota like any upload.
		used, total, err := m.Capacity(ctx, user)
		if err != nil {
			return nil, err
		}
		budget := lim.MaxUncompressed
		if total > 0 {
			budget = total - used
		}
		if budget < 0 {
			budget = 0
		}
		f, _, err := m.ingestContent(ctx, user, parentID, name, tmp, size, budget)
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

	lim := m.archive
	job := m.queue.Enqueue(user.ID, "extract", func(ctx context.Context, report queue.Report) (map[string]any, error) {
		// Cap total extraction time: a gzip bomb is cheap to read but expensive to
		// decompress, so bound CPU/wall-clock regardless of size checks.
		ctx, cancel := context.WithTimeout(ctx, lim.Timeout)
		defer cancel()

		report(10, "Lecture de l’archive")
		tmpPath, err := m.bufferContent(ctx, f)
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmpPath)

		report(30, "Extraction")
		// Budget the extraction against the user's remaining quota, then tighten it
		// with the compression-ratio cap: total uncompressed may not exceed MaxRatio
		// times the compressed archive, but never less than RatioFloor so ordinary
		// small, highly-compressible archives still extract. Folding the ratio into
		// the byte budget means it applies to TAR too (which has no central
		// directory to pre-flight), closing that gap.
		used, total, err := m.Capacity(ctx, user)
		if err != nil {
			return nil, err
		}
		budget := lim.MaxUncompressed
		if total > 0 {
			budget = total - used
		}
		if budget < 0 {
			budget = 0
		}
		if f.Size > 0 {
			ratioCap := lim.RatioFloor
			if f.Size <= math.MaxInt64/lim.MaxRatio {
				if rc := f.Size * lim.MaxRatio; rc > ratioCap {
					ratioCap = rc
				}
			} else {
				ratioCap = lim.MaxUncompressed
			}
			if ratioCap < budget {
				budget = ratioCap
			}
		}

		// Pre-flight for central-directory formats (ZIP, 7z), whose entry sizes are
		// read cheaply: sum the declared uncompressed sizes and reject the whole job
		// up front if it would not fit — so no partial tree is written. TAR has no
		// central directory, so it relies on the per-entry streaming bounds below.
		// total is the number of file entries, when known up front (central-directory
		// formats), used to report a real extraction percentage. TAR streams, so it
		// stays 0 and progress is reported as indeterminate (a running file count).
		fileTotal := 0
		if fm := archive.Format(f.Name); fm == archive.FormatZip || fm == archive.Format7z {
			entries, err := archive.List(tmpPath)
			if err != nil {
				return nil, err
			}
			if len(entries) > lim.MaxEntries {
				return nil, ErrTooManyFiles
			}
			var declared int64
			for _, e := range entries {
				if e.IsDir {
					continue
				}
				fileTotal++
				if e.Size <= 0 {
					continue
				}
				declared += e.Size
				if declared < 0 || declared > budget { // overflow or over budget
					return nil, ErrArchiveTooLarge
				}
			}
		}

		// Track what we create so a failure mid-extraction (timeout, a lying header,
		// or a TAR that overruns) leaves no partial tree: both the files and the
		// folders we made are purged, and never a folder that was already there.
		var createdFiles []uint
		var createdDirs []uint
		count := 0
		skipped := 0
		err = archive.Extract(tmpPath, func(e archive.Entry, open func() (io.ReadCloser, error)) error {
			if err := ctx.Err(); err != nil {
				return err // extraction timed out
			}
			if count >= lim.MaxEntries {
				return ErrTooManyFiles
			}
			// A symlink, device or FIFO has no equivalent here; writing an empty
			// file in its place would be a lie, so it is counted and skipped.
			if e.Unsupported {
				skipped++
				return nil
			}
			clean, ok := archive.SanitizePath(e.Name)
			if !ok {
				// Nothing can be written under a name that is pure traversal.
				skipped++
				return nil
			}
			if e.IsDir {
				_, err := m.mkdirs(ctx, user, destParentID, clean, &createdDirs)
				return err
			}
			dir, base := path.Split(clean)
			parent := destParentID
			if d := strings.Trim(dir, "/"); d != "" {
				p, err := m.mkdirs(ctx, user, destParentID, d, &createdDirs)
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
			file, written, err := m.ingestContent(ctx, user, parent, base, rc, e.Size, budget)
			if err != nil {
				return err
			}
			createdFiles = append(createdFiles, file.ID)
			budget -= written
			count++
			pct := -1 // indeterminate (streaming formats)
			if fileTotal > 0 {
				pct = count * 100 / fileTotal
			}
			report(pct, fmt.Sprintf("%d fichier(s) extrait(s)", count))
			return nil
		})
		if err != nil {
			// Roll back partial output: purge removes the physical objects, the
			// rows, and restores the quota counter (best-effort). Files first,
			// then the folders we created, deepest last-created first — so a
			// parent is never purged while a child still points at it.
			purge := context.WithoutCancel(ctx)
			if len(createdFiles) > 0 {
				_ = m.Purge(purge, user, createdFiles)
			}
			for i := len(createdDirs) - 1; i >= 0; i-- {
				_ = m.Purge(purge, user, []uint{createdDirs[i]})
			}
			return nil, err
		}
		return map[string]any{"count": count, "skipped": skipped}, nil
	})
	return job, nil
}

// ArchiveListing is an archive's index, with a flag for when it was cut short.
type ArchiveListing struct {
	Entries []archive.Entry `json:"entries"`
	// Truncated reports that the archive holds more entries than the limit
	// allows, so what is listed is only the beginning.
	Truncated bool `json:"truncated"`
}

// ListArchiveEntries returns the entries of an archive file without extracting
// it, up to the configured entry limit — an archive with millions of members
// must not turn its preview into a multi-megabyte response.
func (m *Manager) ListArchiveEntries(ctx context.Context, user *model.User, fileID uint) (*ArchiveListing, error) {
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

	entries, err := archive.List(tmpPath)
	if err != nil {
		return nil, err
	}
	if len(entries) > m.archive.MaxEntries {
		return &ArchiveListing{Entries: entries[:m.archive.MaxEntries], Truncated: true}, nil
	}
	return &ArchiveListing{Entries: entries}, nil
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
// at rest when the policy requires it, and updates the user's used storage. The
// extraction is bounded by budget (remaining quota): a declared size over budget
// is rejected up front, and the decompressed stream is capped so a lying header
// (zip bomb) cannot overrun.
//
// What lands in the file's size and in the quota is what was *read*, not what
// the archive claimed: storage backends copy the stream and ignore the size
// they are handed, so trusting a header that over-declares left a file whose
// recorded size did not match its content and a quota counter drifting upwards.
// It returns that byte count.
func (m *Manager) ingestContent(ctx context.Context, user *model.User, parentID *uint, name string, r io.Reader, size, budget int64) (*model.File, int64, error) {
	name = strings.TrimSpace(name)
	if err := validateName(name); err != nil {
		return nil, 0, err
	}
	if size < 0 {
		size = 0
	}
	// Reject before reading a single byte when the declared size already exceeds
	// what is left (honest-huge or overlapping-header bombs).
	if size > budget {
		return nil, 0, ErrArchiveTooLarge
	}
	// Cap the actual decompressed stream at the declared size when known, else at
	// the remaining budget, so a stream that keeps expanding past its header is
	// aborted rather than written to disk.
	limit := size
	if limit == 0 {
		limit = budget
	}
	// Counted on the plaintext, before any encryption wrapper, so the number
	// means the same thing as every other size in the drive.
	bounded := &boundedReader{r: r, max: limit}
	r = bounded

	policy, err := m.policyForUser(ctx, user)
	if err != nil {
		return nil, 0, err
	}
	h, err := m.driverForPolicy(policy)
	if err != nil {
		return nil, 0, err
	}

	var props model.JSON
	if m.policyEncrypts(policy) {
		if m.cipher == nil {
			return nil, 0, errors.New("policy requests encryption but no encryption key is configured")
		}
		if !h.Capabilities().LocalServe {
			return nil, 0, errors.New("encryption is only supported on local-serve storage policies")
		}
		enc, iv, err := m.cipher.EncryptReader(r)
		if err != nil {
			return nil, 0, err
		}
		r = enc
		props = model.MustJSON(model.EntityProps{IV: base64.StdEncoding.EncodeToString(iv)})
	}

	source := newSourcePath(user.ID, name)
	if err := h.Put(ctx, source, r, size); err != nil {
		return nil, 0, err
	}
	written := bounded.n

	entity := &model.Entity{
		Type:            model.EntityTypeVersion,
		Source:          source,
		Size:            written,
		ReferenceCount:  1,
		StoragePolicyID: policy.ID,
		CreatedByID:     user.ID,
		Props:           props,
	}
	if err := m.repo.Entity.Create(ctx, entity); err != nil {
		return nil, 0, err
	}

	file := &model.File{
		Name:            m.uniqueName(ctx, user, parentID, name),
		Type:            model.FileTypeFile,
		OwnerID:         user.ID,
		ParentID:        parentID,
		PrimaryEntityID: &entity.ID,
		Size:            written,
		StoragePolicyID: policy.ID,
	}
	if err := m.repo.File.Create(ctx, file); err != nil {
		return nil, 0, err
	}
	entity.FileID = &file.ID
	if err := m.repo.Entity.Update(ctx, entity); err != nil {
		return nil, 0, err
	}

	m.addStorage(ctx, user, written)
	return file, written, nil
}

// mkdirs resolves (creating as needed) a slash-separated directory path under
// root and returns the id of the deepest folder. Folders it had to create are
// appended to created, in creation order, so a caller rolling back can remove
// exactly those and leave pre-existing ones alone.
func (m *Manager) mkdirs(ctx context.Context, user *model.User, root *uint, dirPath string, created *[]uint) (*uint, error) {
	parent := root
	for _, part := range strings.Split(dirPath, "/") {
		if part == "" || part == "." {
			continue
		}
		id, made, err := m.getOrCreateFolder(ctx, user, parent, part)
		if err != nil {
			return nil, err
		}
		if made && created != nil {
			*created = append(*created, *id)
		}
		parent = id
	}
	return parent, nil
}

// getOrCreateFolder returns the id of a child folder named name under parentID,
// creating it if missing, and reports whether it was the one to create it.
func (m *Manager) getOrCreateFolder(ctx context.Context, user *model.User, parentID *uint, name string) (id *uint, created bool, err error) {
	if existing, err := m.repo.File.FindChildByName(ctx, user.ID, parentID, name); err == nil {
		if existing.IsFolder() {
			found := existing.ID
			return &found, false, nil
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
		return nil, false, err
	}
	made := f.ID
	return &made, true, nil
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
