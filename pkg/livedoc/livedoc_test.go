package livedoc

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// memStore is an in-memory Store recording what the relay persists.
type memStore struct {
	mu           sync.Mutex
	data         []byte
	saves        int
	lastEntityID uint
}

func (s *memStore) Load(context.Context, uint, uint) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data, nil
}

func (s *memStore) Save(_ context.Context, _, _, entityID uint, content []byte) (uint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	s.lastEntityID = entityID
	s.data = content
	if entityID != 0 {
		return entityID, nil
	}
	return 7, nil
}

func (s *memStore) snapshot() (int, uint, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves, s.lastEntityID, string(s.data)
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// testServer exposes a hub over HTTP, admitting each connection with the session
// its query parameters describe.
func testServer(t *testing.T, store Store, opts Options) (*httptest.Server, *Hub) {
	t.Helper()
	h := NewHub(store, opts, quietLogger())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Serve(w, r, Session{
			FileID:   3,
			OwnerID:  1,
			CanWrite: r.URL.Query().Get("write") != "0",
			Name:     r.URL.Query().Get("name"),
		})
	}))
	t.Cleanup(srv.Close)
	return srv, h
}

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

// awaitControl returns the first control message of the given type.
func awaitControl(t *testing.T, conn *websocket.Conn, want string) *outbound {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if typ != websocket.MessageText {
			continue
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

// awaitBinary returns the next opaque frame.
func awaitBinary(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for a CRDT frame: %v", err)
		}
		if typ == websocket.MessageBinary {
			return data
		}
	}
}

func writeText(t *testing.T, conn *websocket.Conn, raw string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(raw)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func writeBinary(t *testing.T, conn *websocket.Conn, payload []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestOnlyTheFirstPeerIsSeededFromTheFile(t *testing.T) {
	srv, _ := testServer(t, &memStore{data: []byte("# Titre\n")}, Options{})

	first := awaitControl(t, dial(t, srv, "name=Alice"), msgInit)
	if first.Seed == nil || *first.Seed != "# Titre\n" {
		t.Fatalf("the first editor must receive the stored document, got %+v", first.Seed)
	}

	// The second editor converges from the first through the CRDT, so re-reading
	// the file for it would only risk duplicating the content.
	second := awaitControl(t, dial(t, srv, "name=Bob"), msgInit)
	if second.Seed != nil {
		t.Fatalf("a later editor must not be seeded, got %q", *second.Seed)
	}
}

func TestCrdtFramesAreRelayedToTheOtherPeersOnly(t *testing.T) {
	srv, _ := testServer(t, &memStore{}, Options{})

	a := dial(t, srv, "name=Alice")
	awaitControl(t, a, msgInit)
	b := dial(t, srv, "name=Bob")
	awaitControl(t, b, msgInit)
	awaitControl(t, a, msgPeers)

	writeBinary(t, a, []byte{0x00, 0x01, 0x02, 0x03})
	if got := awaitBinary(t, b); string(got) != "\x00\x01\x02\x03" {
		t.Fatalf("relayed frame = %v", got)
	}

	// The sender must not receive its own update back.
	writeText(t, a, `{"t":"save","text":"x","synced":true}`)
	if msg := awaitControl(t, a, msgSaved); msg.Type != msgSaved {
		t.Fatalf("expected the save confirmation, got %q", msg.Type)
	}
}

func TestWriterIsElectedAndReassignedOnLeave(t *testing.T) {
	srv, _ := testServer(t, &memStore{}, Options{})

	a := dial(t, srv, "name=Alice")
	initA := awaitControl(t, a, msgInit)
	if initA.Writer == nil || !*initA.Writer {
		t.Fatal("the first writable editor must be the writer")
	}

	b := dial(t, srv, "name=Bob")
	initB := awaitControl(t, b, msgInit)
	if initB.Writer == nil || *initB.Writer {
		t.Fatal("a second editor must not also be the writer")
	}

	a.CloseNow()
	if msg := awaitControl(t, b, msgWriter); msg.Writer == nil || !*msg.Writer {
		t.Fatalf("the remaining editor must take over the writer role, got %+v", msg.Writer)
	}
}

func TestReadOnlyPeerIsNeverTheWriter(t *testing.T) {
	srv, _ := testServer(t, &memStore{}, Options{})

	viewer := dial(t, srv, "write=0&name=Viewer")
	init := awaitControl(t, viewer, msgInit)
	if init.CanWrite == nil || *init.CanWrite {
		t.Fatal("a read-only visitor must be told it cannot write")
	}
	if init.Writer == nil || *init.Writer {
		t.Fatal("a read-only visitor must never be elected writer")
	}
}

func TestOnlyTheWriterPersists(t *testing.T) {
	store := &memStore{data: []byte("start")}
	srv, _ := testServer(t, store, Options{SaveInterval: 20 * time.Millisecond})

	writer := dial(t, srv, "name=Writer")
	awaitControl(t, writer, msgInit)
	other := dial(t, srv, "name=Other")
	awaitControl(t, other, msgInit)

	// A peer that is not the writer must not be able to overwrite the file.
	writeText(t, other, `{"t":"save","text":"from the wrong peer","synced":true}`)
	writeText(t, writer, `{"t":"save","text":"from the writer","synced":true}`)
	awaitControl(t, writer, msgSaved)

	if saves, _, text := store.snapshot(); saves != 1 || text != "from the writer" {
		t.Fatalf("saves=%d text=%q", saves, text)
	}
}

func TestSnapshotsOverwriteTheSessionVersion(t *testing.T) {
	store := &memStore{}
	srv, _ := testServer(t, store, Options{SaveInterval: 20 * time.Millisecond})

	writer := dial(t, srv, "name=Writer")
	awaitControl(t, writer, msgInit)

	writeText(t, writer, `{"t":"save","text":"one","synced":true}`)
	awaitControl(t, writer, msgSaved)
	if _, entityID, _ := store.snapshot(); entityID != 0 {
		t.Fatalf("the first snapshot must create a version, asked to overwrite %d", entityID)
	}

	writeText(t, writer, `{"t":"save","text":"two","synced":true}`)
	awaitControl(t, writer, msgSaved)
	saves, entityID, text := store.snapshot()
	if saves != 2 || entityID != 7 || text != "two" {
		t.Fatalf("saves=%d entityID=%d text=%q, want the session version overwritten", saves, entityID, text)
	}
}

func TestEmptyDocumentIsRefusedFromAnUnsyncedPeer(t *testing.T) {
	store := &memStore{data: []byte("important notes")}
	srv, _ := testServer(t, store, Options{SaveInterval: 20 * time.Millisecond})

	writer := dial(t, srv, "name=Writer")
	awaitControl(t, writer, msgInit)

	// A replica that has not loaded yet must not be able to truncate the file.
	writeText(t, writer, `{"t":"save","text":""}`)
	// A later, synced save proves the refusal did not just delay the write.
	writeText(t, writer, `{"t":"save","text":"still here","synced":true}`)
	awaitControl(t, writer, msgSaved)

	if saves, _, text := store.snapshot(); saves != 1 || text != "still here" {
		t.Fatalf("saves=%d text=%q, want the empty snapshot dropped", saves, text)
	}
}

func TestEmptyDocumentIsAcceptedFromASyncedPeer(t *testing.T) {
	store := &memStore{data: []byte("to be cleared")}
	srv, _ := testServer(t, store, Options{SaveInterval: 20 * time.Millisecond})

	writer := dial(t, srv, "name=Writer")
	awaitControl(t, writer, msgInit)

	writeText(t, writer, `{"t":"save","text":"","synced":true}`)
	awaitControl(t, writer, msgSaved)
	if _, _, text := store.snapshot(); text != "" {
		t.Fatalf("a synced editor must be able to empty the document, got %q", text)
	}
}

func TestRoomRetiresAndFlushesWhenTheLastPeerLeaves(t *testing.T) {
	store := &memStore{}
	// A long interval proves the final flush happens on retirement, not on tick.
	srv, hub := testServer(t, store, Options{SaveInterval: time.Hour})

	conn := dial(t, srv, "name=Alice")
	awaitControl(t, conn, msgInit)
	writeText(t, conn, `{"t":"save","text":"last words","synced":true}`)
	conn.CloseNow()

	deadline := time.Now().Add(5 * time.Second)
	for hub.Rooms() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.Rooms() != 0 {
		t.Fatal("the room must retire once its last editor leaves")
	}
	for time.Now().Before(deadline) {
		if _, _, text := store.snapshot(); text == "last words" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, _, text := store.snapshot()
	t.Fatalf("the last edit must be persisted, got %q", text)
}

func TestRoomRefusesPeersPastTheCap(t *testing.T) {
	srv, _ := testServer(t, &memStore{}, Options{MaxPeers: 1})

	awaitControl(t, dial(t, srv, "name=First"), msgInit)
	if msg := awaitControl(t, dial(t, srv, "name=Second"), msgError); msg.Message == "" {
		t.Fatal("a refused editor must be told why")
	}
}

func TestReopeningReadsTheSavedDocument(t *testing.T) {
	store := &memStore{data: []byte("# first session\n")}
	srv, hub := testServer(t, store, Options{SaveInterval: 20 * time.Millisecond})

	first := dial(t, srv, "name=Alice")
	awaitControl(t, first, msgInit)
	writeText(t, first, `{"t":"save","text":"# second session\n","synced":true}`)
	awaitControl(t, first, msgSaved)
	first.CloseNow()

	// Wait for the room to retire, so the next peer opens a fresh one and is
	// seeded rather than syncing with a leftover session.
	deadline := time.Now().Add(5 * time.Second)
	for hub.Rooms() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	init := awaitControl(t, dial(t, srv, "name=Bob"), msgInit)
	if init.Seed == nil || *init.Seed != "# second session\n" {
		t.Fatalf("a new session must be seeded from the saved document, got %v", init.Seed)
	}
}
