package sftpserver

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/pkg/sftp"
)

// testEnv builds a migrated sqlite-backed Manager over an isolated temp storage
// dir, plus handlers for a fresh user in the seeded default group/policy.
func testEnv(t *testing.T) (*handlers, *filemanager.Manager, *model.User) {
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
	// Point the seeded local policy at isolated temp storage.
	if err := db.Exec("UPDATE storage_policies SET base_path = ? WHERE id = 1", filepath.Join(dir, "storage")).Error; err != nil {
		t.Fatalf("set base_path: %v", err)
	}

	repo := repository.New(db)
	user := &model.User{Email: "t@example.com", Subject: "t", GroupID: 1}
	if err := repo.User.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, err = repo.User.GetByID(context.Background(), user.ID) // preload Group
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}

	mgr := filemanager.NewManager(repo, cache.NewMemory(), dir, nil, nil)
	return newHandlers(mgr, repo, user, false), mgr, user
}

// put writes data to path through the SFTP handler at the given open flags,
// simulating one client write session (WriteAt in chunks from off, then Close).
func put(t *testing.T, h *handlers, path string, flags uint32, off int64, data []byte) {
	t.Helper()
	r := &sftp.Request{Method: "Put", Filepath: path, Flags: flags}
	w, err := h.Filewrite(r)
	if err != nil {
		t.Fatalf("Filewrite %s: %v", path, err)
	}
	if _, err := w.WriteAt(data, off); err != nil {
		t.Fatalf("WriteAt %s: %v", path, err)
	}
	if err := w.(io.Closer).Close(); err != nil {
		t.Fatalf("Close %s: %v", path, err)
	}
}

func readBack(t *testing.T, h *handlers, mgr *filemanager.Manager, path string) []byte {
	t.Helper()
	f, err := h.resolve(context.Background(), path)
	if err != nil || f == nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	rc, err := mgr.OpenContent(context.Background(), f)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return b
}

const (
	fWrite = 0x00000002 // SSH_FXF_WRITE
	fCreat = 0x00000008 // SSH_FXF_CREAT
	fTrunc = 0x00000010 // SSH_FXF_TRUNC
)

// TestResumeInterruptedUpload proves that re-opening a partially-uploaded file
// without truncation continues where it left off, yielding the full correct
// content — the ".part" resume path — instead of a zero-filled hole.
func TestResumeInterruptedUpload(t *testing.T) {
	h, mgr, _ := testEnv(t)

	full := bytes.Repeat([]byte("ABCDEFGH"), 4096) // 32 KiB
	half := len(full) / 2

	// First attempt: fresh upload of the first half, then the connection drops
	// (Close commits the partial ".part").
	put(t, h, "/big.rar.part", fWrite|fCreat|fTrunc, 0, full[:half])
	if got := readBack(t, h, mgr, "/big.rar.part"); !bytes.Equal(got, full[:half]) {
		t.Fatalf("partial commit: got %d bytes, want %d", len(got), half)
	}

	// Resume: re-open WITHOUT truncation and write the remaining bytes at their
	// offset. The staging file must be seeded with the committed first half.
	put(t, h, "/big.rar.part", fWrite, int64(half), full[half:])

	got := readBack(t, h, mgr, "/big.rar.part")
	if !bytes.Equal(got, full) {
		t.Fatalf("resume: content mismatch (len got=%d want=%d)", len(got), len(full))
	}
}

// TestPosixRenameOverwrites proves posix-rename@openssh.com finalises a ".part"
// onto an existing destination by replacing it (rclone/sshfs finalisation).
func TestPosixRenameOverwrites(t *testing.T) {
	h, mgr, _ := testEnv(t)

	old := bytes.Repeat([]byte("x"), 1024)
	newc := bytes.Repeat([]byte("y"), 2048)
	put(t, h, "/doc.txt", fWrite|fCreat|fTrunc, 0, old)      // existing final file
	put(t, h, "/doc.txt.part", fWrite|fCreat|fTrunc, 0, newc) // new upload staged

	if err := h.Filecmd(&sftp.Request{Method: "PosixRename", Filepath: "/doc.txt.part", Target: "/doc.txt"}); err != nil {
		t.Fatalf("PosixRename: %v", err)
	}
	if got := readBack(t, h, mgr, "/doc.txt"); !bytes.Equal(got, newc) {
		t.Fatalf("posix-rename overwrite: got %d bytes, want %d", len(got), len(newc))
	}
	if f, _ := h.resolve(context.Background(), "/doc.txt.part"); f != nil {
		t.Fatalf(".part should be gone after rename")
	}
}

// TestPlainRenameFailsOnExistingDest confirms non-POSIX Rename keeps the
// traditional "conflict if dest exists" behaviour (unchanged).
func TestPlainRenameFailsOnExistingDest(t *testing.T) {
	h, _, _ := testEnv(t)
	put(t, h, "/a.txt", fWrite|fCreat|fTrunc, 0, []byte("a"))
	put(t, h, "/b.txt", fWrite|fCreat|fTrunc, 0, []byte("b"))
	if err := h.Filecmd(&sftp.Request{Method: "Rename", Filepath: "/b.txt", Target: "/a.txt"}); err == nil {
		t.Fatalf("plain Rename onto existing dest should fail")
	}
}
