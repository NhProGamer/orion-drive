package board

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// memStore is an in-memory Store recording what the relay snapshots.
type memStore struct {
	mu       sync.Mutex
	data     []byte
	saves    int
	entityID uint
	// lastEntityID is the entity id the relay asked to overwrite on the last save.
	lastEntityID uint
	failNext     bool
}

func (s *memStore) Load(context.Context, uint, uint) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data, nil
}

func (s *memStore) Save(_ context.Context, _, _, entityID uint, scene []byte) (uint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext {
		s.failNext = false
		return 0, fmt.Errorf("storage is down")
	}
	s.saves++
	s.lastEntityID = entityID
	s.data = scene
	if entityID != 0 {
		return entityID, nil
	}
	s.entityID = 42
	return s.entityID, nil
}

func (s *memStore) snapshot() (int, uint, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves, s.lastEntityID, s.data
}

// el builds a raw element with the given merge-relevant fields.
func el(id string, version, nonce int64, index string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"id":%q,"type":"rectangle","version":%d,"versionNonce":%d,"index":%q,"x":1,"y":2}`,
		id, version, nonce, index))
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSupersedesFollowsExcalidrawRule(t *testing.T) {
	base, err := parseElement(el("a", 5, 100, "a0"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{"higher version wins", el("a", 6, 999, "a0"), true},
		{"lower version loses", el("a", 4, 1, "a0"), false},
		{"same version, lower nonce wins", el("a", 5, 99, "a0"), true},
		{"same version, higher nonce loses", el("a", 5, 101, "a0"), false},
		{"identical is not an update", el("a", 5, 100, "a0"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cand, err := parseElement(tc.raw)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := cand.supersedes(base); got != tc.want {
				t.Fatalf("supersedes = %v, want %v", got, tc.want)
			}
		})
	}
	if next, _ := parseElement(el("a", 1, 1, "a0")); !next.supersedes(nil) {
		t.Fatal("a new element must always be accepted")
	}
}

func TestParseElementRejectsIdlessElement(t *testing.T) {
	if _, err := parseElement(json.RawMessage(`{"type":"rectangle","version":1}`)); err == nil {
		t.Fatal("an element without an id must be rejected")
	}
}

func TestSceneRoundTripKeepsOrderBackgroundAndFiles(t *testing.T) {
	doc := []byte(`{"type":"excalidraw","version":2,"elements":[
		{"id":"b","version":1,"versionNonce":1,"index":"a2"},
		{"id":"a","version":1,"versionNonce":1,"index":"a1"},
		{"id":"gone","version":9,"versionNonce":1,"index":"a3","isDeleted":true}
	],"appState":{"viewBackgroundColor":"#101010"},"files":{"f1":{"mimeType":"image/png"}}}`)

	elements, background, files, err := decodeScene(doc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if background != "#101010" {
		t.Fatalf("background = %q", background)
	}
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	if len(elements) != 3 {
		t.Fatalf("elements = %d, want 3 (tombstone included in memory)", len(elements))
	}

	out, err := encodeScene(elements, background, files)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got document
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if got.Type != SceneFormat || got.Version != sceneVersion {
		t.Fatalf("header = %q/%d", got.Type, got.Version)
	}
	if len(got.Elements) != 2 {
		t.Fatalf("saved %d elements, want the 2 live ones (tombstone dropped)", len(got.Elements))
	}
	var ids []string
	for _, raw := range got.Elements {
		var h elementHeader
		if err := json.Unmarshal(raw, &h); err != nil {
			t.Fatalf("element: %v", err)
		}
		ids = append(ids, h.ID)
	}
	if ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("z-order = %v, want [a b] (sorted by fractional index)", ids)
	}
	if got.AppState.ViewBackgroundColor != "#101010" {
		t.Fatalf("background lost: %q", got.AppState.ViewBackgroundColor)
	}
	if _, ok := got.Files["f1"]; !ok {
		t.Fatal("binary asset lost")
	}
}

func TestDecodeSceneEmptyFileIsEmptyBoard(t *testing.T) {
	elements, background, files, err := decodeScene([]byte("  \n"))
	if err != nil {
		t.Fatalf("an empty file must decode as a blank board: %v", err)
	}
	if len(elements) != 0 || len(files) != 0 || background != defaultBackground {
		t.Fatalf("elements=%d files=%d background=%q", len(elements), len(files), background)
	}
}

func TestDecodeSceneRejectsForeignDocument(t *testing.T) {
	if _, _, _, err := decodeScene([]byte(`{"type":"excalidrawlib","elements":[]}`)); err == nil {
		t.Fatal("a non-Excalidraw document must be refused")
	}
}

func TestEmptySceneIsAValidDocument(t *testing.T) {
	elements, background, _, err := decodeScene(EmptyScene())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(elements) != 0 || background != defaultBackground {
		t.Fatalf("elements=%d background=%q", len(elements), background)
	}
}

// newTestRoom builds a room without going through the hub's socket plumbing.
func newTestRoom(t *testing.T, store Store, opts Options) *room {
	t.Helper()
	h := NewHub(store, opts, quietLogger())
	r, err := newRoom(context.Background(), h, 7, 3)
	if err != nil {
		t.Fatalf("newRoom: %v", err)
	}
	t.Cleanup(r.close)
	return r
}

func TestApplyUpdateReturnsOnlyAcceptedElements(t *testing.T) {
	r := newTestRoom(t, &memStore{}, Options{})

	if got := r.applyUpdate([]json.RawMessage{el("a", 2, 50, "a1")}); len(got) != 1 {
		t.Fatalf("first write accepted %d elements, want 1", len(got))
	}
	// A stale version must not be relayed: the sender reconciles back from the
	// next broadcast instead.
	if got := r.applyUpdate([]json.RawMessage{el("a", 1, 1, "a1")}); len(got) != 0 {
		t.Fatalf("stale update accepted %d elements, want 0", len(got))
	}
	if got := r.applyUpdate([]json.RawMessage{el("a", 2, 10, "a1")}); len(got) != 1 {
		t.Fatalf("lower nonce at equal version must win, accepted %d", len(got))
	}
}

func TestApplyUpdateCapsNewElementsButNotEdits(t *testing.T) {
	r := newTestRoom(t, &memStore{}, Options{MaxElements: 2})

	r.applyUpdate([]json.RawMessage{el("a", 1, 1, "a1"), el("b", 1, 1, "a2")})
	if got := r.applyUpdate([]json.RawMessage{el("c", 1, 1, "a3")}); len(got) != 0 {
		t.Fatal("a new element past the cap must be refused")
	}
	if got := r.applyUpdate([]json.RawMessage{el("a", 2, 1, "a1")}); len(got) != 1 {
		t.Fatal("editing an element already in the scene must stay possible at the cap")
	}
}

func TestApplyFilesIgnoresDuplicatesAndRespectsCap(t *testing.T) {
	r := newTestRoom(t, &memStore{}, Options{MaxFiles: 1})

	if got := r.applyFiles(map[string]json.RawMessage{"f1": json.RawMessage(`{"mimeType":"image/png"}`)}); len(got) != 1 {
		t.Fatalf("first asset added %d, want 1", len(got))
	}
	if got := r.applyFiles(map[string]json.RawMessage{"f1": json.RawMessage(`{"mimeType":"image/gif"}`)}); len(got) != 0 {
		t.Fatal("an id already present must not be overwritten")
	}
	if got := r.applyFiles(map[string]json.RawMessage{"f2": json.RawMessage(`{}`)}); len(got) != 0 {
		t.Fatal("asset past the cap must be refused")
	}
}

func TestFlushOverwritesTheSessionVersion(t *testing.T) {
	store := &memStore{}
	r := newTestRoom(t, store, Options{})

	r.applyUpdate([]json.RawMessage{el("a", 1, 1, "a1")})
	r.flush()
	saves, entityID, data := store.snapshot()
	if saves != 1 || entityID != 0 {
		t.Fatalf("first snapshot: saves=%d entityID=%d, want 1/0 (a new version)", saves, entityID)
	}
	if len(data) == 0 {
		t.Fatal("first snapshot wrote nothing")
	}

	r.applyUpdate([]json.RawMessage{el("a", 2, 1, "a1")})
	r.flush()
	saves, entityID, _ = store.snapshot()
	if saves != 2 || entityID != 42 {
		t.Fatalf("second snapshot: saves=%d entityID=%d, want 2/42 (overwrite in place)", saves, entityID)
	}

	// Nothing changed since: no write at all.
	r.flush()
	if saves, _, _ := store.snapshot(); saves != 2 {
		t.Fatalf("a clean scene must not be written again, saves=%d", saves)
	}
}

func TestFlushRetriesAfterAStorageFailure(t *testing.T) {
	store := &memStore{failNext: true}
	r := newTestRoom(t, store, Options{})

	r.applyUpdate([]json.RawMessage{el("a", 1, 1, "a1")})
	r.flush() // fails
	if saves, _, _ := store.snapshot(); saves != 0 {
		t.Fatalf("saves=%d, want 0", saves)
	}
	r.flush() // the edits must still be pending
	if saves, _, data := store.snapshot(); saves != 1 || len(data) == 0 {
		t.Fatalf("a failed snapshot must be retried, saves=%d bytes=%d", saves, len(data))
	}
}

func TestFlushRefusesToSaveAnOversizedScene(t *testing.T) {
	store := &memStore{}
	r := newTestRoom(t, store, Options{MaxSceneBytes: 64})

	r.applyUpdate([]json.RawMessage{el("a", 1, 1, "a1"), el("b", 1, 1, "a2")})
	r.flush()
	if saves, _, _ := store.snapshot(); saves != 0 {
		t.Fatalf("a scene over the cap must not be written, saves=%d", saves)
	}
}

// --- relay over a real WebSocket ---------------------------------------------

// testServer exposes a hub over HTTP, admitting each connection with the
// session its query parameters describe.
func testServer(t *testing.T, store Store, opts Options) (*httptest.Server, *Hub) {
	t.Helper()
	h := NewHub(store, opts, quietLogger())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Serve(w, r, Session{
			FileID:   7,
			OwnerID:  3,
			CanWrite: r.URL.Query().Get("write") != "0",
			Name:     r.URL.Query().Get("name"),
		})
	}))
	t.Cleanup(srv.Close)
	return srv, h
}

// dial opens a participant connection to the test server.
func dial(t *testing.T, srv *httptest.Server, query string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):]+"?"+query, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

// readUntil returns the first message of the given type, failing on timeout.
func readUntil(t *testing.T, conn *websocket.Conn, want string) *outbound {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		var msg outbound
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Type == want {
			return &msg
		}
	}
}

func writeMsg(t *testing.T, conn *websocket.Conn, msg any) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRelayBroadcastsUpdatesBetweenPeers(t *testing.T) {
	srv, hub := testServer(t, &memStore{}, Options{})

	a := dial(t, srv, "name=Alice")
	initA := readUntil(t, a, msgInit)
	if initA.Self == "" || initA.CanWrite == nil || !*initA.CanWrite {
		t.Fatalf("init = %+v", initA)
	}

	b := dial(t, srv, "name=Bob")
	readUntil(t, b, msgInit)
	// Alice learns about Bob.
	peers := readUntil(t, a, msgPeers)
	if len(peers.Peers) != 2 {
		t.Fatalf("peers = %d, want 2", len(peers.Peers))
	}
	if hub.Rooms() != 1 {
		t.Fatalf("rooms = %d, want 1 (both peers share the board)", hub.Rooms())
	}

	writeMsg(t, a, inbound{Type: msgUpdate, Elements: []json.RawMessage{el("a", 1, 5, "a1")}})
	got := readUntil(t, b, msgUpdate)
	if len(got.Elements) != 1 || got.From != initA.Self {
		t.Fatalf("relayed = %+v", got)
	}

	// A late joiner receives the element in its initial scene.
	c := dial(t, srv, "name=Carol")
	initC := readUntil(t, c, msgInit)
	if initC.Scene == nil || len(initC.Scene.Elements) != 1 {
		t.Fatalf("late joiner scene = %+v", initC.Scene)
	}
}

func TestRelayRefusesWritesFromAReadOnlyPeer(t *testing.T) {
	srv, _ := testServer(t, &memStore{}, Options{})

	writer := dial(t, srv, "name=Owner")
	readUntil(t, writer, msgInit)

	viewer := dial(t, srv, "write=0&name=Viewer")
	initViewer := readUntil(t, viewer, msgInit)
	if initViewer.CanWrite == nil || *initViewer.CanWrite {
		t.Fatal("a read-only participant must be told it cannot write")
	}
	readUntil(t, writer, msgPeers)

	// The viewer's cursor is relayed…
	writeMsg(t, viewer, inbound{Type: msgPointer, X: 10, Y: 20, State: "active"})
	if got := readUntil(t, writer, msgPointer); got.X != 10 {
		t.Fatalf("pointer = %+v", got)
	}
	// …but its element write is not, and never reaches the scene.
	writeMsg(t, viewer, inbound{Type: msgUpdate, Elements: []json.RawMessage{el("x", 1, 1, "a1")}})
	writeMsg(t, viewer, inbound{Type: msgPointer, X: 11, Y: 21, State: "active"})
	if got := readUntil(t, writer, msgPointer); got.X != 11 {
		t.Fatalf("expected the next pointer, got %+v", got)
	}

	fresh := dial(t, srv, "name=Check")
	initFresh := readUntil(t, fresh, msgInit)
	if initFresh.Scene == nil || len(initFresh.Scene.Elements) != 0 {
		t.Fatalf("a read-only peer changed the scene: %+v", initFresh.Scene)
	}
}

func TestRelayRefusesPeersPastTheCap(t *testing.T) {
	srv, _ := testServer(t, &memStore{}, Options{MaxPeers: 1})

	first := dial(t, srv, "name=First")
	readUntil(t, first, msgInit)

	second := dial(t, srv, "name=Second")
	if got := readUntil(t, second, msgError); got.Message == "" {
		t.Fatal("a refused participant must be told why")
	}
}

func TestRoomRetiresAndSavesWhenTheLastPeerLeaves(t *testing.T) {
	store := &memStore{}
	srv, hub := testServer(t, store, Options{SaveInterval: 20 * time.Millisecond})

	conn := dial(t, srv, "name=Alice")
	readUntil(t, conn, msgInit)
	writeMsg(t, conn, inbound{Type: msgUpdate, Elements: []json.RawMessage{el("a", 1, 5, "a1")}})
	readUntil(t, conn, msgSaved)

	conn.CloseNow()

	deadline := time.Now().Add(5 * time.Second)
	for hub.Rooms() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.Rooms() != 0 {
		t.Fatal("the room must retire once its last participant leaves")
	}
	if saves, _, data := store.snapshot(); saves == 0 || len(data) == 0 {
		t.Fatalf("the scene must be persisted, saves=%d", saves)
	}

	// Reopening loads the saved scene back.
	again := dial(t, srv, "name=Alice")
	init := readUntil(t, again, msgInit)
	if init.Scene == nil || len(init.Scene.Elements) != 1 {
		t.Fatalf("reopened scene = %+v", init.Scene)
	}
}

func TestRelayRelaysBackgroundAndAssets(t *testing.T) {
	srv, _ := testServer(t, &memStore{}, Options{})

	a := dial(t, srv, "name=Alice")
	readUntil(t, a, msgInit)
	b := dial(t, srv, "name=Bob")
	readUntil(t, b, msgInit)

	writeMsg(t, a, inbound{Type: msgState, Background: "#222222"})
	if got := readUntil(t, b, msgState); got.Background != "#222222" {
		t.Fatalf("background = %q", got.Background)
	}
	writeMsg(t, a, inbound{Type: msgFiles, Files: map[string]json.RawMessage{"f1": json.RawMessage(`{"mimeType":"image/png"}`)}})
	if got := readUntil(t, b, msgFiles); len(got.Files) != 1 {
		t.Fatalf("assets = %v", got.Files)
	}
}
