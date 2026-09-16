package controllers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/board"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/NhProGamer/orion-drive/service/share"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

// boardEnv builds a Controller with the whiteboard relay enabled, backed by a
// migrated sqlite DB and isolated storage, plus an HTTP server exposing the two
// board endpoints (authenticated and share).
func boardEnv(t *testing.T) (*Controller, *filemanager.Manager, *model.User, *httptest.Server) {
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
	user := &model.User{Email: "b@example.com", Subject: "b", GroupID: 1}
	if err := repo.User.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, _ = repo.User.GetByID(context.Background(), user.ID)

	mgr := filemanager.NewManager(repo, cache.NewMemory(), dir, nil, nil)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	dep := &bootstrap.Dependency{
		Config: cfg,
		Logger: quiet,
		Repo:   repo,
		Files:  mgr,
		Shares: share.New(repo, mgr),
		Cache:  cache.NewMemory(),
		// A short save interval keeps the snapshot assertions quick.
		Boards: board.NewHub(mgr.Snapshots(), board.Options{SaveInterval: 20 * time.Millisecond}, quiet),
	}
	ctl := New(dep)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		// Signed-in routes only: the share endpoint ignores the session.
		middleware.SetUser(c, user)
		c.Next()
	})
	r.GET("/api/v1/file/board/:id/ws", ctl.BoardSocket)
	r.GET("/api/v1/share/:token/board/ws", ctl.BoardSocketShare)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return ctl, mgr, user, srv
}

// boardFrame is the subset of the relay protocol these tests assert on.
type boardFrame struct {
	Type     string `json:"t"`
	Self     string `json:"self"`
	CanWrite *bool  `json:"can_write"`
	Message  string `json:"message"`
	Scene    *struct {
		Elements []json.RawMessage `json:"elements"`
	} `json:"scene"`
}

// createBoard makes an empty whiteboard through the API and returns it.
func createBoard(t *testing.T, ctl *Controller, user *model.User, name string) fileDTO {
	t.Helper()
	c, w := authReq(user, "POST", "/api/v1/file/board/new", `{"parent":"root","name":"`+name+`"}`)
	ctl.NewBoard(c)
	e := decode(t, w)
	if e.Code != 0 {
		t.Fatalf("NewBoard: %s", w.Body.String())
	}
	return dtoOf(t, e)
}

// dialBoard opens a relay connection to path on the test server.
func dialBoard(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):]+path, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

// awaitFrame returns the first frame of the given type.
func awaitFrame(t *testing.T, conn *websocket.Conn, want string) boardFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		var f boardFrame
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		if f.Type == want {
			return f
		}
	}
}

func sendFrame(t *testing.T, conn *websocket.Conn, raw string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(raw)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readBoard returns the stored document of a board.
func readBoard(t *testing.T, mgr *filemanager.Manager, user *model.User, id uint) []byte {
	t.Helper()
	rc, _, err := mgr.OpenFileContent(context.Background(), user, id)
	if err != nil {
		t.Fatalf("open content: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	return data
}

func TestNewBoardCreatesAnEmptyScene(t *testing.T) {
	ctl, mgr, user, _ := boardEnv(t)

	dto := createBoard(t, ctl, user, "Idées")
	if dto.Name != "Idées.excalidraw" {
		t.Fatalf("name = %q, want the whiteboard extension appended", dto.Name)
	}
	var doc struct {
		Type     string            `json:"type"`
		Elements []json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal(readBoard(t, mgr, user, dto.ID), &doc); err != nil {
		t.Fatalf("stored document is not JSON: %v", err)
	}
	if doc.Type != board.SceneFormat || len(doc.Elements) != 0 {
		t.Fatalf("stored document = %+v, want an empty excalidraw scene", doc)
	}
}

func TestBoardSocketRefusesANonWhiteboard(t *testing.T) {
	ctl, mgr, user, _ := boardEnv(t)

	txt, err := mgr.WriteFile(context.Background(), user, nil, "notes.md", strings.NewReader("# hello"), 7)
	if err != nil {
		t.Fatalf("write text file: %v", err)
	}
	c, w := authReq(user, "GET", "/api/v1/file/board/"+utoa(txt.ID)+"/ws", "")
	c.Params = gin.Params{{Key: "id", Value: utoa(txt.ID)}}
	ctl.BoardSocket(c)
	if e := decode(t, w); e.Code == 0 {
		t.Fatalf("a text file must not open as a whiteboard: %s", w.Body.String())
	}
}

func TestBoardSocketRelaysAndPersists(t *testing.T) {
	ctl, mgr, user, srv := boardEnv(t)
	dto := createBoard(t, ctl, user, "Plan")

	owner := dialBoard(t, srv, "/api/v1/file/board/"+utoa(dto.ID)+"/ws")
	init := awaitFrame(t, owner, "init")
	if init.CanWrite == nil || !*init.CanWrite {
		t.Fatal("the owner must join with write access")
	}
	if init.Scene == nil || len(init.Scene.Elements) != 0 {
		t.Fatalf("a fresh board must start empty, got %+v", init.Scene)
	}

	sendFrame(t, owner, `{"t":"update","elements":[{"id":"r1","type":"rectangle","version":3,"versionNonce":7,"index":"a1"}]}`)
	awaitFrame(t, owner, "saved")

	var doc struct {
		Elements []struct {
			ID string `json:"id"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(readBoard(t, mgr, user, dto.ID), &doc); err != nil {
		t.Fatalf("stored document: %v", err)
	}
	if len(doc.Elements) != 1 || doc.Elements[0].ID != "r1" {
		t.Fatalf("stored elements = %+v, want the relayed rectangle", doc.Elements)
	}
}

func TestShareBoardSocketHonoursThePermission(t *testing.T) {
	ctl, _, user, srv := boardEnv(t)
	dto := createBoard(t, ctl, user, "Atelier")
	ctx := context.Background()

	read, err := ctl.dep.Shares.Create(ctx, user, share.CreateOptions{FileID: dto.ID, Permission: model.SharePermRead})
	if err != nil {
		t.Fatalf("create read share: %v", err)
	}
	write, err := ctl.dep.Shares.Create(ctx, user, share.CreateOptions{FileID: dto.ID, Permission: model.SharePermWrite})
	if err != nil {
		t.Fatalf("create write share: %v", err)
	}

	viewer := dialBoard(t, srv, "/api/v1/share/"+read.Token+"/board/ws?name=Visiteur")
	if f := awaitFrame(t, viewer, "init"); f.CanWrite == nil || *f.CanWrite {
		t.Fatal("a read share must join read-only")
	}
	editor := dialBoard(t, srv, "/api/v1/share/"+write.Token+"/board/ws?name=Editeur")
	if f := awaitFrame(t, editor, "init"); f.CanWrite == nil || !*f.CanWrite {
		t.Fatal("a write share must be able to draw")
	}

	// A read-only visitor's element write must not reach the other participant.
	sendFrame(t, viewer, `{"t":"update","elements":[{"id":"x","version":1,"versionNonce":1,"index":"a1"}]}`)
	sendFrame(t, viewer, `{"t":"pointer","x":5,"y":6,"state":"active"}`)
	if f := awaitFrame(t, editor, "pointer"); f.Type != "pointer" {
		t.Fatalf("expected the pointer frame, got %q", f.Type)
	}
	// Reaching the pointer means the (earlier) update was dropped on the way.
	late := dialBoard(t, srv, "/api/v1/share/"+read.Token+"/board/ws")
	if f := awaitFrame(t, late, "init"); f.Scene == nil || len(f.Scene.Elements) != 0 {
		t.Fatalf("a read-only visitor changed the scene: %+v", f.Scene)
	}
}

func TestShareBoardSocketRefusesADepositShare(t *testing.T) {
	ctl, _, user, _ := boardEnv(t)
	dto := createBoard(t, ctl, user, "Boîte")

	folder, err := ctl.dep.Files.CreateFolder(context.Background(), user, nil, "depot")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if err := ctl.dep.Files.Move(context.Background(), user, []uint{dto.ID}, &folder.ID); err != nil {
		t.Fatalf("move board: %v", err)
	}
	deposit, err := ctl.dep.Shares.Create(context.Background(), user, share.CreateOptions{
		FileID:     folder.ID,
		Permission: model.SharePermDeposit,
	})
	if err != nil {
		t.Fatalf("create deposit share: %v", err)
	}

	c, w := authReq(nil, "GET", "/api/v1/share/"+deposit.Token+"/board/ws?path=Bo%C3%AEte.excalidraw", "")
	c.Params = gin.Params{{Key: "token", Value: deposit.Token}}
	ctl.BoardSocketShare(c)
	if e := decode(t, w); e.Code == 0 {
		t.Fatalf("a blind drop box must not expose its whiteboards: %s", w.Body.String())
	}
}

func TestBoardDisabledRefusesTheSocket(t *testing.T) {
	ctl, _, user, _ := boardEnv(t)
	dto := createBoard(t, ctl, user, "Off")
	ctl.dep.Boards = nil

	c, w := authReq(user, "GET", "/api/v1/file/board/"+utoa(dto.ID)+"/ws", "")
	c.Params = gin.Params{{Key: "id", Value: utoa(dto.ID)}}
	ctl.BoardSocket(c)
	if e := decode(t, w); e.Code == 0 {
		t.Fatal("collaboration disabled must refuse the connection")
	}
}

func TestPeerNameIsSanitised(t *testing.T) {
	if got := peerName("  Alice\n"); got != "Alice" {
		t.Fatalf("peerName = %q", got)
	}
	if got := peerName("A\x00B\x1bC"); got != "ABC" {
		t.Fatalf("control characters must be dropped, got %q", got)
	}
	long := peerName(strings.Repeat("é", 100))
	if n := len([]rune(long)); n != maxPeerNameRunes {
		t.Fatalf("name truncated to %d runes, want %d", n, maxPeerNameRunes)
	}
}

func TestBoardSnapshotsReuseTheSessionVersion(t *testing.T) {
	ctl, mgr, user, _ := boardEnv(t)
	dto := createBoard(t, ctl, user, "Session")
	ctx := context.Background()
	store := mgr.Snapshots()

	first, err := store.Save(ctx, user.ID, dto.ID, 0, []byte(`{"type":"excalidraw","elements":[]}`))
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	if first == 0 {
		t.Fatal("the first snapshot must report the version it created")
	}
	_, afterFirst, err := mgr.ListVersions(ctx, user, dto.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}

	scene := []byte(`{"type":"excalidraw","elements":[{"id":"a","version":1,"versionNonce":1}]}`)
	second, err := store.Save(ctx, user.ID, dto.ID, first, scene)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if second != first {
		t.Fatalf("second snapshot wrote version %d, want %d overwritten in place", second, first)
	}
	_, afterSecond, err := mgr.ListVersions(ctx, user, dto.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(afterSecond) != len(afterFirst) {
		t.Fatalf("history grew from %d to %d entries; a drawing session must leave one",
			len(afterFirst), len(afterSecond))
	}
	if got := readBoard(t, mgr, user, dto.ID); string(got) != string(scene) {
		t.Fatalf("stored content = %s", got)
	}

	// A version that is no longer the file's current one cannot be rewritten:
	// the snapshot appends instead, so restoring history stays safe.
	restored, err := store.Save(ctx, user.ID, dto.ID, 99999, scene)
	if err != nil {
		t.Fatalf("stale-version snapshot: %v", err)
	}
	if restored == 99999 {
		t.Fatal("a stale version id must not be reused")
	}
	if _, afterThird, _ := mgr.ListVersions(ctx, user, dto.ID); len(afterThird) != len(afterSecond)+1 {
		t.Fatalf("expected a new version to be appended, history = %d", len(afterThird))
	}
}

func TestBoardSnapshotRefusesALockedFile(t *testing.T) {
	ctl, mgr, user, _ := boardEnv(t)
	dto := createBoard(t, ctl, user, "Verrou")
	ctx := context.Background()

	if _, err := mgr.Lock(ctx, user, dto.ID); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := mgr.Snapshots().Save(ctx, user.ID, dto.ID, 0, board.EmptyScene()); err == nil {
		t.Fatal("a locked file must not be overwritten by a snapshot")
	}
}
