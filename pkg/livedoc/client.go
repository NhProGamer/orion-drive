package livedoc

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// outboxSize is how many frames may queue for one peer. CRDT updates arrive at
// typing speed, so a peer whose socket cannot keep up is disconnected rather
// than allowed to grow a backlog — it reconnects and re-syncs, which is cheaper
// than buffering a stale stream.
const outboxSize = 128

// writeTimeout bounds a single frame write to one peer.
const writeTimeout = 10 * time.Second

// peerColors tint remote cursors and names.
var peerColors = []string{
	"#7c6cf0", "#2f9e68", "#e0803a", "#d05074",
	"#3b82c4", "#b45fc0", "#c0a02f", "#3aa8a0",
}

// frame is one queued outbound message, text (control) or binary (CRDT).
type frame struct {
	data   []byte
	binary bool
}

// client is one connected editor.
type client struct {
	room *room
	conn *websocket.Conn
	id   string
	name string

	mu       sync.Mutex
	canWrite bool
	writer   bool

	out    chan frame
	closed sync.Once
	done   chan struct{}
}

func newClient(r *room, conn *websocket.Conn, s Session) *client {
	name := s.Name
	if name == "" {
		name = "Invité"
	}
	return &client{
		room:     r,
		conn:     conn,
		id:       randomID(),
		name:     name,
		canWrite: s.CanWrite,
		out:      make(chan frame, outboxSize),
		done:     make(chan struct{}),
	}
}

func (c *client) peer() *Peer {
	return &Peer{ID: c.id, Name: c.name, Color: colorFor(c.id), Write: c.writable()}
}

func (c *client) writable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.canWrite
}

func (c *client) setWritable(v bool) {
	c.mu.Lock()
	c.canWrite = v
	c.mu.Unlock()
}

func (c *client) isWriter() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writer
}

// setWriter records the peer's role and reports whether it changed, so only a
// real transition is announced.
func (c *client) setWriter(v bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writer == v {
		return false
	}
	c.writer = v
	return true
}

// send queues a control frame, dropping the peer when its outbox is full.
func (c *client) send(data []byte) { c.enqueue(frame{data: data}) }

// sendBinary queues an opaque CRDT frame.
func (c *client) sendBinary(data []byte) { c.enqueue(frame{data: data, binary: true}) }

func (c *client) enqueue(f frame) {
	select {
	case c.out <- f:
	case <-c.done:
	default:
		c.stop() // too slow: cut it loose, the client reconnects and re-syncs
	}
}

func (c *client) sendMsg(msg *outbound) {
	if data, err := marshal(msg); err == nil {
		c.send(data)
	}
}

func (c *client) stop() {
	c.closed.Do(func() { close(c.done) })
}

// writeLoop drains the outbox onto the socket.
func (c *client) writeLoop(ctx context.Context) {
	for {
		select {
		case f := <-c.out:
			typ := websocket.MessageText
			if f.binary {
				typ = websocket.MessageBinary
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(wctx, typ, f.data)
			cancel()
			if err != nil {
				c.stop()
				return
			}
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// pingLoop keeps the connection alive through idle proxies and notices a peer
// that vanished without closing, so its cursor stops haunting the document.
func (c *client) pingLoop(ctx context.Context) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Ping(pctx)
			cancel()
			if err != nil {
				c.stop()
				return
			}
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// revalidateLoop re-checks the authorisation that admitted this peer. A revoked
// share ends the session; one downgraded to read-only drops the write
// permission (and the writer role with it) without kicking the visitor out.
func (c *client) revalidateLoop(ctx context.Context, cancel context.CancelFunc, check func(context.Context) (bool, bool)) {
	t := time.NewTicker(revalidateInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			canWrite, ok := check(ctx)
			if !ok {
				c.sendMsg(&outbound{Type: msgError, Message: "access to this document has been revoked"})
				// Let the frame reach the socket before tearing the session down.
				time.Sleep(200 * time.Millisecond)
				cancel()
				c.stop()
				return
			}
			if canWrite != c.writable() {
				c.setWritable(canWrite)
				write := canWrite
				c.sendMsg(&outbound{Type: msgReadOnly, CanWrite: &write})
				c.room.reelect()
				c.room.broadcast(&outbound{Type: msgPeers, Peers: c.room.peerList()}, "")
			}
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// readLoop handles the peer's frames until it disconnects.
func (c *client) readLoop(ctx context.Context) {
	for {
		select {
		case <-c.done:
			return
		case <-ctx.Done():
			return
		default:
		}

		typ, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			// A CRDT update: relayed verbatim, never inspected. Read-only peers
			// still send awareness (their cursor), which changes no content.
			c.room.relay(data, c.id)
		case websocket.MessageText:
			var msg inbound
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			c.handle(&msg)
		}
	}
}

// handle applies one control message.
func (c *client) handle(msg *inbound) {
	if msg.Type != msgSave {
		return
	}
	// Only the elected writer persists, and only with write permission — both
	// re-read here so a mid-session downgrade or re-election takes effect on the
	// very next frame.
	if !c.writable() || !c.isWriter() {
		return
	}
	// An empty document is the one destructive write, so it is only accepted
	// from a peer that knows its replica is real rather than still loading.
	if msg.Text == "" && !msg.Synced {
		c.room.hub.log.Warn("refused an empty document snapshot from an unsynced peer",
			"file_id", c.room.fileID)
		return
	}
	c.room.stage(msg.Text)
}

// colorFor derives a stable cursor colour from a peer id.
func colorFor(id string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return peerColors[int(h.Sum32())%len(peerColors)]
}

func marshal(msg *outbound) ([]byte, error) { return json.Marshal(msg) }
