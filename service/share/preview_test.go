package share_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/NhProGamer/orion-drive/service/share"
)

// previewEnv builds a migrated sqlite-backed share Service over isolated local
// storage, plus a user and Manager.
func previewEnv(t *testing.T) (*share.Service, *filemanager.Manager, *repository.Repository, *model.User) {
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
	user := &model.User{Email: "o@example.com", Subject: "o", GroupID: 1}
	if err := repo.User.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, _ = repo.User.GetByID(context.Background(), user.ID)
	mgr := filemanager.NewManager(repo, cache.NewMemory(), dir, nil, nil)
	return share.New(repo, mgr), mgr, repo, user
}

func writeFile(t *testing.T, mgr *filemanager.Manager, user *model.User, name string, data []byte) *model.File {
	t.Helper()
	f, err := mgr.WriteFile(context.Background(), user, nil, name, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return f
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png: %v", err)
	}
	return buf.Bytes()
}

// TestMetaDoesNotCountView proves Meta (used by the OG preview) leaves the view
// counter untouched, while View records one — so crawls/link unfurls don't
// inflate the count.
func TestMetaDoesNotCountView(t *testing.T) {
	svc, mgr, repo, user := previewEnv(t)
	f := writeFile(t, mgr, user, "photo.png", pngBytes(t))
	sh, err := svc.Create(context.Background(), user, share.CreateOptions{FileID: f.ID})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	if _, err := svc.Meta(context.Background(), sh.Token); err != nil {
		t.Fatalf("meta: %v", err)
	}
	if _, err := svc.Meta(context.Background(), sh.Token); err != nil {
		t.Fatalf("meta2: %v", err)
	}
	after, _ := repo.Share.GetByToken(context.Background(), sh.Token)
	if after.Views != 0 {
		t.Fatalf("Meta must not count views, got %d", after.Views)
	}
	if _, err := svc.View(context.Background(), sh.Token); err != nil {
		t.Fatalf("view: %v", err)
	}
	after, _ = repo.Share.GetByToken(context.Background(), sh.Token)
	if after.Views != 1 {
		t.Fatalf("View should count 1, got %d", after.Views)
	}
}

// TestMetaPreviewable proves a thumbnailable image share is flagged previewable,
// a plain text share is not.
func TestMetaPreviewable(t *testing.T) {
	svc, mgr, _, user := previewEnv(t)

	img := writeFile(t, mgr, user, "photo.png", pngBytes(t))
	shImg, _ := svc.Create(context.Background(), user, share.CreateOptions{FileID: img.ID})
	v, err := svc.Meta(context.Background(), shImg.Token)
	if err != nil {
		t.Fatalf("meta img: %v", err)
	}
	if !v.Previewable {
		t.Fatalf("image share should be previewable")
	}
	if v.Name != "photo.png" {
		t.Fatalf("name should be revealed, got %q", v.Name)
	}

	txt := writeFile(t, mgr, user, "notes.txt", []byte("hello"))
	shTxt, _ := svc.Create(context.Background(), user, share.CreateOptions{FileID: txt.ID})
	v, _ = svc.Meta(context.Background(), shTxt.Token)
	if v.Previewable {
		t.Fatalf("text share should not be previewable")
	}
}

// TestMetaSuppressedWhenProtected proves a password-protected share leaks no
// name/owner/previewable via Meta (same gating as View).
func TestMetaSuppressedWhenProtected(t *testing.T) {
	svc, mgr, _, user := previewEnv(t)
	f := writeFile(t, mgr, user, "secret.png", pngBytes(t))
	sh, _ := svc.Create(context.Background(), user, share.CreateOptions{FileID: f.ID, Password: "hunter2"})

	v, err := svc.Meta(context.Background(), sh.Token)
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if v.Name != "" || v.Owner != "" || v.Previewable {
		t.Fatalf("protected share must not leak metadata: %+v", v)
	}
	if !v.HasPassword {
		t.Fatalf("HasPassword flag should be set")
	}
}

// TestInlineDoesNotCount proves inline preview streams the file without
// consuming a download slot, while an explicit Download does.
func TestInlineDoesNotCount(t *testing.T) {
	svc, mgr, repo, user := previewEnv(t)
	f := writeFile(t, mgr, user, "photo.png", pngBytes(t))
	sh, err := svc.Create(context.Background(), user, share.CreateOptions{FileID: f.ID, MaxDownloads: 5})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	// Two previews: the counter must not move.
	for i := 0; i < 2; i++ {
		tgt, err := svc.Inline(context.Background(), sh.Token, "", "")
		if err != nil {
			t.Fatalf("inline: %v", err)
		}
		if tgt.Stream != nil {
			tgt.Stream.Close()
		}
	}
	after, _ := repo.Share.GetByToken(context.Background(), sh.Token)
	if after.Downloads != 0 {
		t.Fatalf("inline must not count, downloads=%d", after.Downloads)
	}

	// An explicit download meters one.
	tgt, err := svc.Download(context.Background(), sh.Token, "", "")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if tgt.Stream != nil {
		tgt.Stream.Close()
	}
	after, _ = repo.Share.GetByToken(context.Background(), sh.Token)
	if after.Downloads != 1 {
		t.Fatalf("download should count 1, got %d", after.Downloads)
	}
}

// TestInlineRefusedWhenProtected proves inline is gated like a download: a
// password-protected share yields nothing without the password.
func TestInlineRefusedWhenProtected(t *testing.T) {
	svc, mgr, _, user := previewEnv(t)
	f := writeFile(t, mgr, user, "secret.png", pngBytes(t))
	sh, _ := svc.Create(context.Background(), user, share.CreateOptions{FileID: f.ID, Password: "p"})
	if _, err := svc.Inline(context.Background(), sh.Token, "", ""); err == nil {
		t.Fatalf("inline without password must be refused")
	}
}

// TestShareThumbnail proves the public thumbnail returns image bytes for an open
// image share and is refused for a password-protected one.
func TestShareThumbnail(t *testing.T) {
	svc, mgr, _, user := previewEnv(t)

	f := writeFile(t, mgr, user, "photo.png", pngBytes(t))
	open, _ := svc.Create(context.Background(), user, share.CreateOptions{FileID: f.ID})
	data, err := svc.Thumbnail(context.Background(), open.Token, "")
	if err != nil {
		t.Fatalf("thumbnail: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected thumbnail bytes")
	}

	f2 := writeFile(t, mgr, user, "locked.png", pngBytes(t))
	prot, _ := svc.Create(context.Background(), user, share.CreateOptions{FileID: f2.ID, Password: "x"})
	if _, err := svc.Thumbnail(context.Background(), prot.Token, ""); err == nil {
		t.Fatalf("protected share thumbnail must be refused")
	}
}
