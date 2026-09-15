package board

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Store persists board scenes. It is implemented against the file manager by the
// caller so this package needs no knowledge of storage policies, encryption or
// quotas.
type Store interface {
	// Load returns the raw .excalidraw document, or an empty slice for a board
	// whose file has no content yet.
	Load(ctx context.Context, ownerID, fileID uint) ([]byte, error)
	// Save writes scene as the file's content. entityID is the storage version a
	// previous call in the same session returned (0 on the first save): passing
	// it back asks for that version to be overwritten instead of a new one being
	// appended. The id of the written version is returned.
	Save(ctx context.Context, ownerID, fileID, entityID uint, scene []byte) (uint, error)
}

// Options bounds what a board may hold and how often it is snapshotted. Zero
// fields fall back to the defaults below.
type Options struct {
	// MaxSceneBytes caps both a single inbound WebSocket frame and the saved
	// document, so a peer cannot grow a board without limit.
	MaxSceneBytes int64
	// MaxElements caps how many elements one board holds.
	MaxElements int
	// MaxFiles caps how many binary assets (images) one board holds.
	MaxFiles int
	// MaxPeers caps concurrent participants per board.
	MaxPeers int
	// SaveInterval is the shortest delay between two snapshots of a board being
	// actively edited.
	SaveInterval time.Duration
}

// Defaults for Options. The scene cap is deliberately generous: an Excalidraw
// document embeds pasted images as base64 data URLs, so a drawing with a few
// screenshots in it legitimately reaches several megabytes.
const (
	defaultMaxSceneBytes = 32 << 20 // 32 MiB
	defaultMaxElements   = 20000
	defaultMaxFiles      = 200
	defaultMaxPeers      = 30
	defaultSaveInterval  = 5 * time.Second
)

// withDefaults fills unset fields with the built-in defaults.
func (o Options) withDefaults() Options {
	if o.MaxSceneBytes <= 0 {
		o.MaxSceneBytes = defaultMaxSceneBytes
	}
	if o.MaxElements <= 0 {
		o.MaxElements = defaultMaxElements
	}
	if o.MaxFiles <= 0 {
		o.MaxFiles = defaultMaxFiles
	}
	if o.MaxPeers <= 0 {
		o.MaxPeers = defaultMaxPeers
	}
	if o.SaveInterval <= 0 {
		o.SaveInterval = defaultSaveInterval
	}
	return o
}

// ErrRoomFull is returned when a board already has MaxPeers participants.
var ErrRoomFull = errors.New("this board already has the maximum number of editors")

// Hub owns every open board. One room exists per file id, shared by signed-in
// editors and share-link visitors alike, which is what makes a link handed to
// someone else land them on the same canvas.
type Hub struct {
	store Store
	opts  Options
	log   *slog.Logger

	mu    sync.Mutex
	rooms map[uint]*room
}

// NewHub builds a Hub over store.
func NewHub(store Store, opts Options, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		store: store,
		opts:  opts.withDefaults(),
		log:   log,
		rooms: map[uint]*room{},
	}
}

// Session describes one authorised participant. The caller resolves the file and
// the permission (from the session cookie or from a share token) before handing
// the connection over.
type Session struct {
	FileID   uint
	OwnerID  uint
	CanWrite bool
	// Name is shown to the other participants. Empty falls back to a generic
	// label so an anonymous visitor still appears in the cursor list.
	Name string
	// Revalidate, when set, is polled while the session is open to re-check the
	// authorisation that admitted it. Returning ok=false closes the connection,
	// so deleting, expiring or downgrading a share ends live editing instead of
	// letting an open tab keep writing.
	Revalidate func(context.Context) (canWrite bool, ok bool)
}

// revalidateInterval is how often Session.Revalidate is polled.
const revalidateInterval = 30 * time.Second

// pingInterval bounds how long a dead TCP connection keeps a peer in the list.
const pingInterval = 30 * time.Second

// Serve upgrades the request to a WebSocket and runs the participant's session
// until the connection drops. It always writes a response, error or not.
func (h *Hub) Serve(w http.ResponseWriter, req *http.Request, s Session) {
	// The default origin check (Origin host must equal the request host) is kept:
	// a board connection carries the caller's session cookie, so a page on
	// another origin must not be able to open one.
	conn, err := websocket.Accept(w, req, nil)
	if err != nil {
		return // Accept already wrote the failure
	}
	defer conn.CloseNow()
	conn.SetReadLimit(h.opts.MaxSceneBytes)

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	r, err := h.join(ctx, s.FileID, s.OwnerID)
	if err != nil {
		h.log.Warn("board join failed", "file_id", s.FileID, "error", err)
		writeFatal(ctx, conn, "this board could not be opened")
		return
	}
	defer h.leave(r)

	c := newClient(r, conn, s)
	if err := r.add(c); err != nil {
		writeFatal(ctx, conn, err.Error())
		return
	}
	defer r.remove(c)

	go c.writeLoop(ctx)
	if s.Revalidate != nil {
		go c.revalidateLoop(ctx, cancel, s.Revalidate)
	}
	go c.pingLoop(ctx)

	c.sendInit()
	// The joining peer already has the list inside its init frame; only the
	// others need telling.
	r.broadcast(&outbound{Type: msgPeers, Peers: r.peerList()}, c.id)

	c.readLoop(ctx)
}

// join returns the room for a board, loading its scene on first use.
func (h *Hub) join(ctx context.Context, fileID, ownerID uint) (*room, error) {
	for {
		h.mu.Lock()
		if r, ok := h.rooms[fileID]; ok {
			r.mu.Lock()
			retired := r.retired
			r.mu.Unlock()
			h.mu.Unlock()
			if !retired {
				return r, nil
			}
			// The room is being torn down; wait for leave() to drop it and retry
			// so this peer gets a fresh one loaded from the saved scene.
			select {
			case <-r.done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		h.mu.Unlock()

		// Load outside the hub lock: reading the scene hits storage.
		r, err := newRoom(ctx, h, fileID, ownerID)
		if err != nil {
			return nil, err
		}
		h.mu.Lock()
		if existing, ok := h.rooms[fileID]; ok {
			h.mu.Unlock()
			r.close() // lost the race, discard ours
			return existing, nil
		}
		h.rooms[fileID] = r
		h.mu.Unlock()
		return r, nil
	}
}

// leave retires a room once its last participant is gone, so the final snapshot
// is written and the scene stops occupying memory.
func (h *Hub) leave(r *room) {
	h.mu.Lock()
	r.mu.Lock()
	empty := len(r.peers) == 0
	if empty {
		r.retired = true
	}
	r.mu.Unlock()
	if empty && h.rooms[r.fileID] == r {
		delete(h.rooms, r.fileID)
	}
	h.mu.Unlock()
	if empty {
		r.close()
	}
}

// Rooms reports how many boards are currently open (used by the admin stats).
func (h *Hub) Rooms() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}

// add registers a participant, refusing it when the board is full.
func (r *room) add(c *client) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.peers) >= r.hub.opts.MaxPeers {
		return ErrRoomFull
	}
	r.peers[c.id] = c
	return nil
}

// remove drops a participant and tells the others.
func (r *room) remove(c *client) {
	r.mu.Lock()
	if _, ok := r.peers[c.id]; !ok {
		r.mu.Unlock()
		return
	}
	delete(r.peers, c.id)
	peers := r.peerListLocked()
	r.mu.Unlock()
	c.stop()
	r.broadcast(&outbound{Type: msgPeers, Peers: peers}, "")
}

// writeFatal delivers a final error message before the connection closes, so the
// browser can show why instead of a bare disconnect.
func writeFatal(ctx context.Context, conn *websocket.Conn, message string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := marshal(&outbound{Type: msgError, Message: message})
	if err == nil {
		_ = conn.Write(ctx, websocket.MessageText, data)
	}
	_ = conn.Close(websocket.StatusPolicyViolation, "board unavailable")
}

// randomID returns an unguessable peer id. Peer ids are visible to the other
// participants, so they must not be derived from the user.
func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
