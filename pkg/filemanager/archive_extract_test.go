package filemanager_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/archive"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/pkg/queue"
	"github.com/NhProGamer/orion-drive/repository"
)

// env is a manager backed by a migrated sqlite database and isolated storage,
// plus the pieces the extraction path needs (a queue to run its job on).
type env struct {
	mgr    *filemanager.Manager
	repo   *repository.Repository
	queue  *queue.Queue
	user   *model.User
	tmpDir string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	cfg := &conf.Config{}
	cfg.Database.Type = "sqlite"
	cfg.Database.DBFile = filepath.Join(dir, "orion.db")
	db, err := bootstrap.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := bootstrap.Migrate(db, cfg); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec("UPDATE storage_policies SET base_path = ? WHERE id = 1", filepath.Join(dir, "storage")).Error; err != nil {
		t.Fatalf("base_path: %v", err)
	}
	repo := repository.New(db)
	user := &model.User{Email: "a@example.com", Subject: "a", GroupID: 1}
	if err := repo.User.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, _ = repo.User.GetByID(context.Background(), user.ID)

	q := queue.New(1)
	t.Cleanup(q.Close)
	return &env{
		mgr:    filemanager.NewManager(repo, cache.NewMemory(), dir, nil, q),
		repo:   repo,
		queue:  q,
		user:   user,
		tmpDir: dir,
	}
}

// quota caps the user's group storage, which is what bounds an extraction.
func (e *env) quota(t *testing.T, bytes int64) {
	t.Helper()
	group, err := e.repo.Group.GetByID(context.Background(), e.user.GroupID)
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	group.MaxStorage = bytes
	if err := e.repo.Group.Update(context.Background(), group); err != nil {
		t.Fatalf("update group: %v", err)
	}
	e.user, _ = e.repo.User.GetByID(context.Background(), e.user.ID)
}

// upload stores data as a file in the drive root and returns it.
func (e *env) upload(t *testing.T, name string, data []byte) *model.File {
	t.Helper()
	f, err := e.mgr.WriteFile(context.Background(), e.user, nil, name, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("upload %s: %v", name, err)
	}
	return f
}

// extract runs the extraction job to completion and returns it.
func (e *env) extract(t *testing.T, fileID uint, dest *uint) queue.Job {
	t.Helper()
	job, err := e.mgr.Extract(context.Background(), e.user, fileID, dest)
	if err != nil {
		t.Fatalf("schedule extract: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := e.queue.Get(e.user.ID, job.ID)
		if ok && (got.Status == queue.StatusDone || got.Status == queue.StatusFailed) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("extraction did not finish")
	return queue.Job{}
}

// children lists a folder's contents (nil = drive root).
func (e *env) children(t *testing.T, parent *uint) []model.File {
	t.Helper()
	out, err := e.repo.File.ListChildren(context.Background(), e.user.ID, parent)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return out
}

func (e *env) storageUsed(t *testing.T) int64 {
	t.Helper()
	u, err := e.repo.User.GetByID(context.Background(), e.user.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return u.StorageUsed
}

// --- fixtures ---------------------------------------------------------------

// tarball builds an uncompressed TAR from the given headers and bodies.
func tarball(t *testing.T, entries []struct {
	hdr  tar.Header
	body string
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := e.hdr
		if hdr.Typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatalf("tar header %s: %v", hdr.Name, err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("tar body %s: %v", hdr.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	return buf.Bytes()
}

// --- tests ------------------------------------------------------------------

// zipOf builds a ZIP from name/body pairs, in order.
func zipOf(t *testing.T, entries [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatalf("zip header %s: %v", e[0], err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatalf("zip body %s: %v", e[0], err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// TestExtractedSizesMatchTheContent pins what a file's recorded size and the
// quota charge mean: the bytes actually stored.
//
// They are now counted while writing rather than taken from the entry header,
// because storage backends copy the stream and ignore the size they are handed
// — so a header and its content disagreeing would otherwise leave a file whose
// size did not match it and a quota counter drifting. The stdlib zip and tar
// readers happen to refuse such an archive before it reaches here, so this is
// a guard rather than a reproduction: it fails if accounting ever goes back to
// trusting the declaration.
func TestExtractedSizesMatchTheContent(t *testing.T) {
	e := newEnv(t)
	const short, long = "abc", "0123456789"
	f := e.upload(t, "sizes.zip", zipOf(t, [][2]string{
		{"short.txt", short},
		{"nested/long.txt", long},
	}))
	before := e.storageUsed(t)

	if job := e.extract(t, f.ID, nil); job.Status != queue.StatusDone {
		t.Fatalf("extraction failed: %s", job.Error)
	}

	sizes := map[string]int64{}
	for _, c := range e.children(t, nil) {
		if c.Name == "nested" {
			for _, inner := range e.children(t, &c.ID) {
				sizes[inner.Name] = inner.Size
			}
			continue
		}
		sizes[c.Name] = c.Size
	}
	if sizes["short.txt"] != int64(len(short)) {
		t.Fatalf("short.txt recorded %d bytes, want %d", sizes["short.txt"], len(short))
	}
	if sizes["long.txt"] != int64(len(long)) {
		t.Fatalf("nested/long.txt recorded %d bytes, want %d", sizes["long.txt"], len(long))
	}
	if grew := e.storageUsed(t) - before; grew != int64(len(short)+len(long)) {
		t.Fatalf("quota grew by %d, want %d", grew, len(short)+len(long))
	}
}

func TestExtractNormalisesWindowsEntryNames(t *testing.T) {
	e := newEnv(t)
	data := tarball(t, []struct {
		hdr  tar.Header
		body string
	}{
		{tar.Header{Name: `..\..\escaped.txt`, Typeflag: tar.TypeReg, Mode: 0o644}, "out"},
		{tar.Header{Name: `sub\dir\inner.txt`, Typeflag: tar.TypeReg, Mode: 0o644}, "in"},
	})
	f := e.upload(t, "windows.tar", data)

	job := e.extract(t, f.ID, nil)
	if job.Status != queue.StatusDone {
		t.Fatalf("a backslash name must not fail the job: %s", job.Error)
	}

	var sawEscaped, sawSub bool
	for _, c := range e.children(t, nil) {
		switch c.Name {
		case "escaped.txt":
			sawEscaped = true
		case "sub":
			sawSub = true
			names := []string{}
			for _, inner := range e.children(t, &c.ID) {
				names = append(names, inner.Name)
			}
			if len(names) != 1 || names[0] != "dir" {
				t.Fatalf("sub/ holds %v, want [dir]", names)
			}
		}
	}
	if !sawEscaped {
		t.Fatal(`..\..\escaped.txt must land flat at the destination`)
	}
	if !sawSub {
		t.Fatal(`sub\dir\inner.txt must create nested folders`)
	}
}

func TestExtractSkipsEntriesWithNoEquivalent(t *testing.T) {
	e := newEnv(t)
	data := tarball(t, []struct {
		hdr  tar.Header
		body string
	}{
		{tar.Header{Name: "real.txt", Typeflag: tar.TypeReg, Mode: 0o644}, "content"},
		{tar.Header{Name: "link.txt", Typeflag: tar.TypeSymlink, Linkname: "real.txt", Mode: 0o777}, ""},
		{tar.Header{Name: "pipe", Typeflag: tar.TypeFifo, Mode: 0o644}, ""},
	})
	f := e.upload(t, "links.tar", data)

	job := e.extract(t, f.ID, nil)
	if job.Status != queue.StatusDone {
		t.Fatalf("extraction failed: %s", job.Error)
	}
	if got := job.Result["count"]; got != 1 {
		t.Fatalf("extracted %v entries, want 1", got)
	}
	if got := job.Result["skipped"]; got != 2 {
		t.Fatalf("skipped %v entries, want 2", got)
	}
	for _, c := range e.children(t, nil) {
		if c.Name == "link.txt" || c.Name == "pipe" {
			t.Fatalf("%q was materialised; a symlink or FIFO must be skipped, not written empty", c.Name)
		}
	}
}

func TestExtractRollsBackFilesAndFolders(t *testing.T) {
	e := newEnv(t)
	// Two entries under a folder the extraction has to create, and a quota that
	// only the first one fits in — so the job fails partway through.
	data := tarball(t, []struct {
		hdr  tar.Header
		body string
	}{
		{tar.Header{Name: "pack/first.txt", Typeflag: tar.TypeReg, Mode: 0o644}, "small"},
		{tar.Header{Name: "pack/second.txt", Typeflag: tar.TypeReg, Mode: 0o644}, string(bytes.Repeat([]byte("x"), 4096))},
	})
	f := e.upload(t, "rollback.tar", data)
	e.quota(t, int64(len(data))+64) // room for the archive itself and little else
	before := e.storageUsed(t)

	job := e.extract(t, f.ID, nil)
	if job.Status != queue.StatusFailed {
		t.Fatalf("expected the extraction to fail on quota, got %s", job.Status)
	}

	for _, c := range e.children(t, nil) {
		if c.Name == "pack" {
			t.Fatal("the folder the extraction created must be purged too, not left empty")
		}
		if c.Name == "first.txt" {
			t.Fatal("the file written before the failure must be purged")
		}
	}
	if used := e.storageUsed(t); used != before {
		t.Fatalf("quota is %d after the rollback, want %d", used, before)
	}
}

// --- limits -----------------------------------------------------------------

func TestPlanArchiveRefusesTooManyEntries(t *testing.T) {
	e := newEnv(t)
	e.mgr.SetArchiveLimits(filemanager.ArchiveLimits{MaxEntries: 2})

	var ids []uint
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		ids = append(ids, e.upload(t, name, []byte("x")).ID)
	}

	if _, err := e.mgr.PlanArchive(context.Background(), e.user, ids); !errors.Is(err, filemanager.ErrTooManyFiles) {
		t.Fatalf("PlanArchive error = %v, want ErrTooManyFiles", err)
	}
	// Within the limit it measures what the archive would hold.
	plan, err := e.mgr.PlanArchive(context.Background(), e.user, ids[:2])
	if err != nil {
		t.Fatalf("PlanArchive: %v", err)
	}
	if plan.Entries != 2 || plan.Bytes != 2 {
		t.Fatalf("plan = %+v, want 2 entries / 2 bytes", plan)
	}
}

func TestPlanArchiveRefusesTooManyBytes(t *testing.T) {
	e := newEnv(t)
	e.mgr.SetArchiveLimits(filemanager.ArchiveLimits{MaxUncompressed: 8})
	id := e.upload(t, "big.bin", bytes.Repeat([]byte("x"), 32)).ID

	if _, err := e.mgr.PlanArchive(context.Background(), e.user, []uint{id}); !errors.Is(err, filemanager.ErrArchiveTooLarge) {
		t.Fatalf("PlanArchive error = %v, want ErrArchiveTooLarge", err)
	}
}

func TestWriteArchiveRoundTrips(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	folder, err := e.mgr.CreateFolder(ctx, e.user, nil, "pack")
	if err != nil {
		t.Fatalf("folder: %v", err)
	}
	if _, err := e.mgr.WriteFile(ctx, e.user, &folder.ID, "inner.txt", bytes.NewReader([]byte("nested")), 6); err != nil {
		t.Fatalf("inner: %v", err)
	}
	if _, err := e.mgr.CreateFolder(ctx, e.user, &folder.ID, "hollow"); err != nil {
		t.Fatalf("empty folder: %v", err)
	}

	var buf bytes.Buffer
	if err := e.mgr.WriteArchive(ctx, e.user, []uint{folder.ID}, &buf, archive.FormatZip, 0, nil); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	found := map[string]string{}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			found[f.Name] = ""
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		found[f.Name] = string(b)
		// An entry with no mode unpacks unreadable on some extractors.
		if f.Mode().Perm() == 0 {
			t.Errorf("%s carries no file mode", f.Name)
		}
	}
	if found["pack/inner.txt"] != "nested" {
		t.Fatalf("archive = %v, want pack/inner.txt with its content", found)
	}
	if _, ok := found["pack/hollow/"]; !ok {
		t.Fatalf("archive = %v, want the empty folder to survive as an entry", found)
	}
}

func TestListArchiveEntriesIsBounded(t *testing.T) {
	e := newEnv(t)
	e.mgr.SetArchiveLimits(filemanager.ArchiveLimits{MaxEntries: 2})

	f := e.upload(t, "many.zip", zipOf(t, [][2]string{
		{"a.txt", "1"}, {"b.txt", "2"}, {"c.txt", "3"}, {"d.txt", "4"},
	}))
	listing, err := e.mgr.ListArchiveEntries(context.Background(), e.user, f.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listing.Entries) != 2 || !listing.Truncated {
		t.Fatalf("listing = %d entries truncated=%v, want 2/true", len(listing.Entries), listing.Truncated)
	}
}

func TestListingAnArchiveStagesNothingOnDisk(t *testing.T) {
	e := newEnv(t)
	f := e.upload(t, "index.zip", zipOf(t, [][2]string{{"a.txt", "1"}, {"b/c.txt", "2"}}))

	if _, err := e.mgr.ListArchiveEntries(context.Background(), e.user, f.ID); err != nil {
		t.Fatalf("list: %v", err)
	}
	// Listing used to copy the whole object into the staging directory, every
	// time the preview was opened. It now reads the index off the object, so
	// nothing is staged at all.
	staged, err := filepath.Glob(filepath.Join(e.tmpDir, "content-*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(staged) != 0 {
		t.Fatalf("listing staged %v on disk", staged)
	}
}

func TestArchiveFormatIsValidated(t *testing.T) {
	e := newEnv(t)
	id := e.upload(t, "f.txt", []byte("x")).ID

	// An unknown format is a bad request, not a server error: the caller asked
	// for something this build cannot produce.
	if err := e.mgr.WriteArchive(context.Background(), e.user, []uint{id}, &bytes.Buffer{}, "rar", 0, nil); !errors.Is(err, filemanager.ErrArchiveFormat) {
		t.Fatalf("WriteArchive error = %v, want ErrArchiveFormat", err)
	}
	if _, err := e.mgr.Compress(context.Background(), e.user, nil, []uint{id}, "x", "rar", 0); !errors.Is(err, filemanager.ErrArchiveFormat) {
		t.Fatalf("Compress error = %v, want ErrArchiveFormat", err)
	}
	// An empty request means the configured default.
	if got, err := e.mgr.ArchiveFormat(""); err != nil || got != archive.FormatZip {
		t.Fatalf("ArchiveFormat(\"\") = (%q, %v), want zip", got, err)
	}
}

func TestUniqueNameKeepsAMultiPartExtension(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ids := []uint{e.upload(t, "f.txt", []byte("x")).ID}

	// Two archives asked for under the same name: the second has to be renamed,
	// and must stay recognisable as an archive. path.Ext alone would have
	// produced "backup.tar (2).gz", which no reader accepts.
	for i := 0; i < 2; i++ {
		job, err := e.mgr.Compress(ctx, e.user, nil, ids, "backup", archive.FormatTarGz, 0)
		if err != nil {
			t.Fatalf("compress %d: %v", i, err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			got, ok := e.queue.Get(e.user.ID, job.ID)
			if ok && got.Status == queue.StatusDone {
				break
			}
			if ok && got.Status == queue.StatusFailed {
				t.Fatalf("compress %d failed: %s", i, got.Error)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	var names []string
	for _, c := range e.children(t, nil) {
		if c.Name != "f.txt" {
			names = append(names, c.Name)
		}
	}
	sort.Strings(names)
	want := []string{"backup (2).tar.gz", "backup.tar.gz"}
	if len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("archives = %v, want %v", names, want)
	}
	for _, n := range names {
		if archive.Format(n) != archive.FormatTarGz {
			t.Errorf("%q is no longer recognised as a tar.gz", n)
		}
	}
}

func TestCancellingAnExtractionLeavesNothingBehind(t *testing.T) {
	e := newEnv(t)
	// Enough members that the job is still running when the cancel lands.
	entries := make([][2]string, 0, 400)
	body := string(bytes.Repeat([]byte("payload "), 4096)) // 32 KiB each
	for i := 0; i < 400; i++ {
		entries = append(entries, [2]string{fmt.Sprintf("pack/f%03d.txt", i), body})
	}
	f := e.upload(t, "cancel.zip", zipOf(t, entries))
	before := e.storageUsed(t)

	job, err := e.mgr.Extract(context.Background(), e.user, f.ID, nil)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	// Wait for it to be under way, then stop it.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok := e.queue.Get(e.user.ID, job.ID); ok && got.Status == queue.StatusRunning && got.Done > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !e.queue.Cancel(e.user.ID, job.ID) {
		t.Skip("the extraction finished before it could be cancelled; nothing to assert")
	}

	var final queue.Job
	for time.Now().Before(deadline) {
		got, ok := e.queue.Get(e.user.ID, job.ID)
		if ok && (got.Status == queue.StatusDone || got.Status == queue.StatusFailed) {
			final = got
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if final.Status != queue.StatusFailed || final.Error != queue.ErrCancelled.Error() {
		t.Fatalf("job ended as %s / %q, want failed / %q", final.Status, final.Error, queue.ErrCancelled)
	}

	// The whole point: stopping a job must not leave a partial tree.
	for _, c := range e.children(t, nil) {
		if c.Name != "cancel.zip" {
			t.Fatalf("%q survived a cancelled extraction", c.Name)
		}
	}
	if used := e.storageUsed(t); used != before {
		t.Fatalf("quota is %d after the cancellation, want %d", used, before)
	}
}

func TestCancelIgnoresOtherPeoplesJobs(t *testing.T) {
	e := newEnv(t)
	id := e.upload(t, "f.txt", []byte("x")).ID
	job, err := e.mgr.Compress(context.Background(), e.user, nil, []uint{id}, "c", archive.FormatZip, 0)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if e.queue.Cancel(e.user.ID+1, job.ID) {
		t.Fatal("a job was cancelled by someone who does not own it")
	}
}

func TestCompressHonoursTheRequestedLevel(t *testing.T) {
	e := newEnv(t)
	// Text that compresses well, so the levels produce different sizes.
	id := e.upload(t, "text.txt", bytes.Repeat([]byte("the quick brown fox "), 50000)).ID

	sizes := map[int]int64{}
	for _, level := range []int{1, 9} {
		var buf bytes.Buffer
		if err := e.mgr.WriteArchive(context.Background(), e.user, []uint{id}, &buf,
			archive.FormatTarGz, level, nil); err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
		sizes[level] = int64(buf.Len())
	}
	if sizes[9] >= sizes[1] {
		t.Fatalf("level 9 produced %d bytes and level 1 %d; the level is being ignored",
			sizes[9], sizes[1])
	}
}
