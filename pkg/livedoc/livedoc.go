package livedoc

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

// Store persists document snapshots. It is satisfied by the file manager, so
// this package needs no knowledge of storage policies, encryption or quotas.
type Store interface {
	// Load returns the stored document, or an empty slice for a file with no
	// content yet.
	Load(ctx context.Context, ownerID, fileID uint) ([]byte, error)
	// Save writes content as the file's content. entityID is the storage version
	// a previous call in the same session returned (0 on the first save):
	// passing it back asks for that version to be overwritten rather than a new
	// one appended. The id of the written version is returned.
	Save(ctx context.Context, ownerID, fileID, entityID uint, content []byte) (uint, error)
}

// Options bounds a session. Zero fields fall back to the defaults below.
type Options struct {
	// MaxFrameBytes caps a single inbound frame, CRDT update or control message.
	MaxFrameBytes int64
	// MaxPeers caps concurrent editors per document.
	MaxPeers int
	// SaveInterval is the shortest delay between two snapshots of a document
	// being actively edited.
	SaveInterval time.Duration
}

const (
	defaultMaxFrameBytes = 8 << 20 // 8 MiB
	defaultMaxPeers      = 30
	defaultSaveInterval  = 3 * time.Second
)

func (o Options) withDefaults() Options {
	if o.MaxFrameBytes <= 0 {
		o.MaxFrameBytes = defaultMaxFrameBytes
	}
	if o.MaxPeers <= 0 {
		o.MaxPeers = defaultMaxPeers
	}
	if o.SaveInterval <= 0 {
		o.SaveInterval = defaultSaveInterval
	}
	return o
}

// ErrRoomFull is returned when a document already has MaxPeers editors.
var ErrRoomFull = errors.New("this document already has the maximum number of editors")

// Hub owns every open document, one room per file id, shared by signed-in users
// and share-link visitors alike.
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
	return &Hub{store: store, opts: opts.withDefaults(), log: log, rooms: map[uint]*room{}}
}

// Session describes one authorised participant. The caller resolves the file and
// the permission — from the session cookie or from a share token — before
// handing the connection over.
type Session struct {
	FileID   uint
	OwnerID  uint
	CanWrite bool
	Name     string
	// Revalidate, when set, is polled while the session is open to re-check the
	// authorisation that admitted it, so revoking or downgrading a share takes
	// effect on an already-open tab.
	Revalidate func(context.Context) (canWrite bool, ok bool)
}

const (
	// revalidateInterval is how often Session.Revalidate is polled.
	revalidateInterval = 30 * time.Second
	// pingInterval bounds how long a dead connection keeps a peer in the list.
	pingInterval = 30 * time.Second
)

// Serve upgrades the request to a WebSocket and runs the session until the
// connection drops. It always writes a response, error or not.
func (h *Hub) Serve(w http.ResponseWriter, req *http.Request, s Session) {
	// The default origin check (Origin host must equal the request host) is
	// kept: the connection carries the caller's session cookie, so a page on
	// another origin must not be able to open one.
	conn, err := websocket.Accept(w, req, nil)
	if err != nil {
		return // Accept already wrote the failure
	}
	defer conn.CloseNow()
	conn.SetReadLimit(h.opts.MaxFrameBytes)

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	r := h.join(s.FileID, s.OwnerID)
	defer h.leave(r)

	c := newClient(r, conn, s)
	seed, err := r.add(c)
	if err != nil {
		writeFatal(ctx, conn, err.Error())
		return
	}
	defer r.remove(c)

	go c.writeLoop(ctx)
	go c.pingLoop(ctx)
	if s.Revalidate != nil {
		go c.revalidateLoop(ctx, cancel, s.Revalidate)
	}

	// The document is only read from storage for the peer that opened an empty
	// room; everyone after it converges from the peers already editing.
	var seedText *string
	if seed {
		data, err := h.store.Load(ctx, s.OwnerID, s.FileID)
		if err != nil {
			h.log.Warn("document load failed", "file_id", s.FileID, "error", err)
			writeFatal(ctx, conn, "this document could not be opened")
			return
		}
		text := string(data)
		seedText = &text
	}

	write, writer := c.writable(), c.isWriter()
	c.sendMsg(&outbound{
		Type:     msgInit,
		Self:     c.id,
		CanWrite: &write,
		Writer:   &writer,
		Seed:     seedText,
		Peers:    r.peerList(),
	})
	// The joining peer already has the list in its init frame.
	r.broadcast(&outbound{Type: msgPeers, Peers: r.peerList()}, c.id)

	c.readLoop(ctx)
}

// join returns the room for a document, creating it on first use. Unlike the
// whiteboard hub this never touches storage, so there is no slow path to keep
// outside the lock.
func (h *Hub) join(fileID, ownerID uint) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.rooms[fileID]; ok {
		r.mu.Lock()
		retired := r.retired
		r.mu.Unlock()
		if !retired {
			return r
		}
	}
	r := newRoom(h, fileID, ownerID)
	h.rooms[fileID] = r
	return r
}

// leave retires a room once its last peer is gone, so the final snapshot is
// written and the next session starts from the saved file.
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

// Rooms reports how many documents are currently open.
func (h *Hub) Rooms() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}

// writeFatal delivers a final error before closing, so the browser can say why.
func writeFatal(ctx context.Context, conn *websocket.Conn, message string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if data, err := marshal(&outbound{Type: msgError, Message: message}); err == nil {
		_ = conn.Write(ctx, websocket.MessageText, data)
	}
	_ = conn.Close(websocket.StatusPolicyViolation, "document unavailable")
}

// randomID returns an unguessable peer id. Peer ids are visible to the other
// participants, so they must not be derived from the user.
func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
