package controllers

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/NhProGamer/orion-drive/service/share"
	"github.com/gin-gonic/gin"
)

// ogEnv builds a Controller backed by a migrated sqlite DB + isolated storage.
func ogEnv(t *testing.T) (*Controller, *filemanager.Manager, *model.User) {
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
	dep := &bootstrap.Dependency{Config: cfg, Repo: repo, Files: mgr, Shares: share.New(repo, mgr)}
	return New(dep), mgr, user
}

func ogPNG(t *testing.T) []byte {
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

func ogCtx() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "http://drive.example.com/s/x", nil)
	return c
}

// TestShareOGTags_Image proves an image share yields a large-image card with the
// file name, owner and an absolute thumbnail URL.
func TestShareOGTags_Image(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	f, err := mgr.WriteFile(context.Background(), user, nil, "photo.png", bytes.NewReader(ogPNG(t)), int64(len(ogPNG(t))))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	sh, _ := ctl.dep.Shares.Create(context.Background(), user, share.CreateOptions{FileID: f.ID})

	tags := ctl.shareOGTags(ogCtx(), sh.Token)
	for _, want := range []string{
		`<meta property="og:title" content="photo.png">`,
		`<meta property="og:type" content="website">`,
		`<meta property="og:url" content="http://drive.example.com/s/` + sh.Token + `">`,
		`<meta property="og:image" content="http://drive.example.com/api/v1/share/` + sh.Token + `/thumb">`,
		`<meta name="twitter:card" content="summary_large_image">`,
	} {
		if !strings.Contains(tags, want) {
			t.Errorf("missing tag %q in:\n%s", want, tags)
		}
	}
}

// TestShareOGTags_Text proves a non-previewable file yields a plain summary card
// with no og:image.
func TestShareOGTags_Text(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	f, _ := mgr.WriteFile(context.Background(), user, nil, "notes.txt", strings.NewReader("hi"), 2)
	sh, _ := ctl.dep.Shares.Create(context.Background(), user, share.CreateOptions{FileID: f.ID})

	tags := ctl.shareOGTags(ogCtx(), sh.Token)
	if strings.Contains(tags, "og:image") {
		t.Errorf("text share must have no og:image:\n%s", tags)
	}
	if !strings.Contains(tags, `<meta name="twitter:card" content="summary">`) {
		t.Errorf("expected summary card:\n%s", tags)
	}
}

// TestShareOGTags_Protected proves a password-protected share emits no tags (no
// metadata leak; the generic app card stands).
func TestShareOGTags_Protected(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	f, _ := mgr.WriteFile(context.Background(), user, nil, "secret.png", bytes.NewReader(ogPNG(t)), int64(len(ogPNG(t))))
	sh, _ := ctl.dep.Shares.Create(context.Background(), user, share.CreateOptions{FileID: f.ID, Password: "p"})

	if tags := ctl.shareOGTags(ogCtx(), sh.Token); tags != "" {
		t.Errorf("protected share should emit no tags, got:\n%s", tags)
	}
}

func TestInjectHead(t *testing.T) {
	page := []byte(`<!doctype html><html><head><title>x</title></head><body>hi</body></html>`)
	out := string(injectHead(page, `<meta property="og:title" content="T">`))
	if !strings.Contains(out, `<meta property="og:title" content="T"></head>`) {
		t.Fatalf("tag not injected before </head>: %s", out)
	}

	// No head close tag → unchanged.
	noHead := []byte(`<html><body>x</body></html>`)
	if got := injectHead(noHead, "<meta>"); string(got) != string(noHead) {
		t.Fatalf("expected unchanged when no </head>, got %s", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:               "0 o",
		512:             "512 o",
		1024:            "1.0 ko",
		1536:            "1.5 ko",
		1024 * 1024:     "1.0 Mo",
		5 * 1024 * 1024: "5.0 Mo",
	}
	for n, want := range cases {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
