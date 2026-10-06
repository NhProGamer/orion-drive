package repository_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/repository"
)

func testRepo(t *testing.T) *repository.Repository {
	t.Helper()
	dir := t.TempDir()
	cfg := &conf.Config{}
	cfg.Database.Type = "sqlite"
	cfg.Database.DBFile = filepath.Join(dir, "t.db")
	db, err := bootstrap.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := bootstrap.Migrate(db, cfg); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return repository.New(db)
}

// TestFileOwnerScoping is the core security invariant: a file is only reachable
// by its owner. GetByID and FindChildByName must not leak across owners.
func TestFileOwnerScoping(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	f := &model.File{Name: "a.txt", Type: model.FileTypeFile, OwnerID: 1}
	if err := repo.File.Create(ctx, f); err != nil {
		t.Fatalf("create: %v", err)
	}

	if got, err := repo.File.GetByID(ctx, 1, f.ID); err != nil || got.Name != "a.txt" {
		t.Fatalf("owner should read own file: %v", err)
	}
	if _, err := repo.File.GetByID(ctx, 2, f.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("another owner must get ErrNotFound, got %v", err)
	}
	if _, err := repo.File.FindChildByName(ctx, 1, nil, "a.txt"); err != nil {
		t.Fatalf("owner FindChildByName: %v", err)
	}
	if _, err := repo.File.FindChildByName(ctx, 2, nil, "a.txt"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("another owner FindChildByName must not find it, got %v", err)
	}
}

// TestShareReserveDownload proves the atomic download-counter reservation caps a
// limited share and increments the counter, and refund restores a slot.
func TestShareReserveDownload(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()

	one := 1
	limited := &model.Share{Token: "tok-limited", FileID: 1, UserID: 1, RemainDownloads: &one}
	if err := repo.Share.Create(ctx, limited); err != nil {
		t.Fatalf("create: %v", err)
	}
	if ok, _ := repo.Share.ReserveDownload(ctx, limited); !ok {
		t.Fatalf("first reserve should succeed")
	}
	if ok, _ := repo.Share.ReserveDownload(ctx, limited); ok {
		t.Fatalf("second reserve on a 1-download share must fail")
	}
	if err := repo.Share.RefundDownload(ctx, limited); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if ok, _ := repo.Share.ReserveDownload(ctx, limited); !ok {
		t.Fatalf("reserve after refund should succeed")
	}

	unlimited := &model.Share{Token: "tok-unlimited", FileID: 1, UserID: 1}
	if err := repo.Share.Create(ctx, unlimited); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < 3; i++ {
		if ok, _ := repo.Share.ReserveDownload(ctx, unlimited); !ok {
			t.Fatalf("unlimited share reserve should always succeed")
		}
	}
	got, err := repo.Share.GetByToken(ctx, "tok-unlimited")
	if err != nil || got.Downloads != 3 {
		t.Fatalf("downloads = %d (err %v), want 3", got.Downloads, err)
	}
}

// TestAPITokenGetByHash proves the Bearer auth lookup finds a token by hash and
// returns ErrNotFound for an unknown hash (so auth fails cleanly).
func TestAPITokenGetByHash(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	if err := repo.APIToken.Create(ctx, &model.APIToken{UserID: 5, TokenHash: "deadbeef", Label: "cli"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.APIToken.GetByHash(ctx, "deadbeef")
	if err != nil || got.UserID != 5 {
		t.Fatalf("get by hash: %v (uid %d)", err, got.UserID)
	}
	if _, err := repo.APIToken.GetByHash(ctx, "unknown"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown hash must be ErrNotFound, got %v", err)
	}
}

// TestUserAddStorage proves the atomic storage counter accumulates deltas and
// clamps at zero (never goes negative), the invariant that replaced the racy
// in-memory read-modify-write.
func TestUserAddStorage(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	u := &model.User{Email: "s@t.u", Subject: "sub-store", GroupID: 1}
	if err := repo.User.Create(ctx, u); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repo.User.AddStorage(ctx, u.ID, 1000); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.User.AddStorage(ctx, u.ID, 500); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got, _ := repo.User.GetByID(ctx, u.ID); got.StorageUsed != 1500 {
		t.Fatalf("after +1000 +500, storage = %d, want 1500", got.StorageUsed)
	}
	// Over-subtracting must clamp at zero, not underflow negative.
	if err := repo.User.AddStorage(ctx, u.ID, -5000); err != nil {
		t.Fatalf("sub: %v", err)
	}
	if got, _ := repo.User.GetByID(ctx, u.ID); got.StorageUsed != 0 {
		t.Fatalf("after -5000, storage = %d, want 0 (clamped)", got.StorageUsed)
	}
}

// TestAncestors proves the breadcrumb trail is rebuilt root-first for a folder
// (including itself) and for a file (its parent chain), and stays owner-scoped.
func TestAncestors(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	a := &model.File{Name: "A", Type: model.FileTypeFolder, OwnerID: 1}
	if err := repo.File.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	b := &model.File{Name: "B", Type: model.FileTypeFolder, OwnerID: 1, ParentID: &a.ID}
	if err := repo.File.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	doc := &model.File{Name: "doc.txt", Type: model.FileTypeFile, OwnerID: 1, ParentID: &b.ID}
	if err := repo.File.Create(ctx, doc); err != nil {
		t.Fatal(err)
	}

	// Folder → trail includes itself.
	tr, err := repo.File.Ancestors(ctx, 1, b.ID)
	if err != nil || len(tr) != 2 || tr[0].Name != "A" || tr[1].Name != "B" {
		t.Fatalf("folder ancestors = %+v (err %v), want [A B]", tr, err)
	}
	// File → trail is its parent folders.
	tr, err = repo.File.Ancestors(ctx, 1, doc.ID)
	if err != nil || len(tr) != 2 || tr[1].Name != "B" {
		t.Fatalf("file ancestors = %+v (err %v), want [A B]", tr, err)
	}
	// Another owner must not resolve it.
	if _, err := repo.File.Ancestors(ctx, 2, b.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-owner ancestors must be ErrNotFound, got %v", err)
	}
}

// TestUserLookup covers the email/id lookups used at login and in auth.
func TestUserLookup(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	u := &model.User{Email: "x@y.z", Subject: "sub1", GroupID: 1}
	if err := repo.User.Create(ctx, u); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got, err := repo.User.GetByEmail(ctx, "x@y.z"); err != nil || got.ID != u.ID {
		t.Fatalf("get by email: %v", err)
	}
	if _, err := repo.User.GetByEmail(ctx, "nobody@nowhere.z"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown email must be ErrNotFound, got %v", err)
	}
}

// TestSettingUpsert checks an unset key reads as "" and Set overwrites in place.
func TestSettingUpsert(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	if v, err := repo.Setting.Get(ctx, "custom_css"); err != nil || v != "" {
		t.Fatalf("unset key = %q, %v; want \"\", nil", v, err)
	}
	for _, want := range []string{"body{color:red}", ":root{--accent:#0f0}"} {
		if err := repo.Setting.Set(ctx, "custom_css", want); err != nil {
			t.Fatalf("set: %v", err)
		}
		if v, err := repo.Setting.Get(ctx, "custom_css"); err != nil || v != want {
			t.Fatalf("get = %q, %v; want %q", v, err, want)
		}
	}
}

// TestSiteAssetPutListDelete checks an upload replaces in place, List skips
// the image data, and Delete restores "not set".
func TestSiteAssetPutListDelete(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	for _, etag := range []string{"aaa", "bbb"} {
		a := &model.SiteAsset{Name: "favicon", ContentType: "image/png", ETag: etag, Data: []byte(etag)}
		if err := repo.SiteAsset.Put(ctx, a); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	got, err := repo.SiteAsset.Get(ctx, "favicon")
	if err != nil || got.ETag != "bbb" || string(got.Data) != "bbb" {
		t.Fatalf("get = %+v, %v; want the second upload", got, err)
	}
	list, err := repo.SiteAsset.List(ctx)
	if err != nil || len(list) != 1 || list[0].Data != nil {
		t.Fatalf("list = %+v, %v; want one entry without data", list, err)
	}
	if err := repo.SiteAsset.Delete(ctx, "favicon"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.SiteAsset.Get(ctx, "favicon"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("get after delete = %v; want ErrNotFound", err)
	}
}
