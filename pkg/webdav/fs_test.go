package webdav

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/NhProGamer/orion-drive/migrations"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	_ "github.com/NhProGamer/orion-drive/pkg/filemanager/driver/local" // register the local storage driver
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMapErr(t *testing.T) {
	cases := []struct {
		in   error
		want error
	}{
		{nil, nil},
		{filemanager.ErrNotFound, os.ErrNotExist},
		{repository.ErrNotFound, os.ErrNotExist},
		{filemanager.ErrConflict, os.ErrExist},
		{filemanager.ErrInvalidName, os.ErrInvalid},
	}
	for _, c := range cases {
		if got := mapErr(c.in); !errors.Is(got, c.want) {
			t.Errorf("mapErr(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	// An unmapped error passes through unchanged.
	custom := errors.New("boom")
	if got := mapErr(custom); got != custom {
		t.Errorf("unmapped error should pass through, got %v", got)
	}
}

// testFS builds an FS over a migrated sqlite Manager with a user in context.
func testFS(t *testing.T) (*FS, context.Context) {
	t.Helper()
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "t.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	goose.SetBaseFS(migrations.FS)
	_ = goose.SetDialect("sqlite3")
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(sqlDB, migrations.Dir("sqlite3")); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec("UPDATE storage_policies SET base_path = ? WHERE id = 1", filepath.Join(dir, "storage")).Error; err != nil {
		t.Fatalf("base_path: %v", err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	user := &model.User{Email: "w@x.y", Subject: "w", GroupID: 1}
	if err := repo.User.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, _ = repo.User.GetByID(ctx, user.ID)
	mgr := filemanager.NewManager(repo, cache.NewMemory(), dir, nil, nil)
	return NewFS(mgr, repo), withUser(ctx, user)
}

// TestFSLifecycle drives the WebDAV filesystem through mkdir → write → stat →
// rename → remove against a real Manager, and checks error mapping.
func TestFSLifecycle(t *testing.T) {
	fs, ctx := testFS(t)

	if err := fs.Mkdir(ctx, "/docs", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	w, err := fs.OpenFile(ctx, "/docs/a.txt", os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for write: %v", err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	fi, err := fs.Stat(ctx, "/docs/a.txt")
	if err != nil || fi.Size() != 5 || fi.IsDir() {
		t.Fatalf("stat: fi=%v err=%v", fi, err)
	}

	// Read the content back.
	r, err := fs.OpenFile(ctx, "/docs/a.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open for read: %v", err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != "hello" {
		t.Fatalf("content = %q", b)
	}

	// Missing path maps to os.ErrNotExist.
	if _, err := fs.Stat(ctx, "/docs/missing.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing stat = %v, want ErrNotExist", err)
	}

	if err := fs.Rename(ctx, "/docs/a.txt", "/docs/b.txt"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := fs.Stat(ctx, "/docs/b.txt"); err != nil {
		t.Fatalf("stat after rename: %v", err)
	}

	if err := fs.RemoveAll(ctx, "/docs/b.txt"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := fs.Stat(ctx, "/docs/b.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat after remove = %v, want ErrNotExist", err)
	}
}
