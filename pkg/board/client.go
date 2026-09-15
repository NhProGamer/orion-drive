package board

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// outboxSize is how many frames may queue for one peer. Pointer frames arrive at
// cursor speed, so a peer whose socket cannot keep up is disconnected rather
// than allowed to grow an unbounded backlog — it reconnects and gets a fresh
// scene, which is cheaper than buffering a stale one.
const outboxSize = 64

// writeTimeout bounds a single frame write to one peer.
const writeTimeout = 10 * time.Second

// peerColors tint remote cursors. Excalidraw shows the colour next to the name.
var peerColors = []string{
	"#7c6cf0", "#2f9e68", "#e0803a", "#d05074",
	"#3b82c4", "#b45fc0", "#c0a02f", "#3aa8a0",
}

// client is one connected participant of a room.
type client struct {
	room *room
	conn *websocket.Conn
	id   string
	name string

	mu       sync.Mutex
	canWrite bool

	out    chan []byte
	closed sync.Once
	done   chan struct{}
}

// newClient wraps an accepted connection as a room participant.
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
		out:      make(chan []byte, outboxSize),
		done:     make(chan struct{}),
	}
}

// peer projects the participant as advertised to the others.
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

// send queues a frame, dropping the participant when its outbox is full.
func (c *client) send(data []byte) {
	select {
	case c.out <- data:
	case <-c.done:
	default:
		c.stop() // too slow: cut it loose, the client will reconnect
	}
}

// sendMsg queues an encoded message.
func (c *client) sendMsg(msg *outbound) {
	if data, err := marshal(msg); err == nil {
		c.send(data)
	}
}

// sendInit delivers the full scene and the peer list to a freshly joined
// participant.
func (c *client) sendInit() {
	write := c.writable()
	c.sendMsg(&outbound{
		Type:     msgInit,
		Self:     c.id,
		CanWrite: &write,
		Scene:    c.room.snapshot(),
		Peers:    c.room.peerList(),
	})
}

// stop releases the participant's write loop.
func (c *client) stop() {
	c.closed.Do(func() { close(c.done) })
}

// writeLoop drains the outbox onto the socket.
func (c *client) writeLoop(ctx context.Context) {
	for {
		select {
		case data := <-c.out:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(wctx, websocket.MessageText, data)
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

// pingLoop keeps the connection alive through idle proxies and, more
// importantly, notices a peer that vanished without closing so its cursor stops
// haunting the board.
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

// revalidateLoop re-checks the authorisation that admitted this participant.
// A revoked, expired or deleted share ends the session; a share downgraded to
// read-only drops the write permission without kicking the visitor out.
func (c *client) revalidateLoop(ctx context.Context, cancel context.CancelFunc, check func(context.Context) (bool, bool)) {
	t := time.NewTicker(revalidateInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			canWrite, ok := check(ctx)
			if !ok {
				c.sendMsg(&outbound{Type: msgError, Message: "access to this board has been revoked"})
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
				c.room.broadcast(&outbound{Type: msgPeers, Peers: c.room.peerList()}, "")
			}
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// readLoop handles the participant's messages until it disconnects. It returns
// on any read error, which is the normal way a session ends.
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
			if isOverLimit(err) {
				c.room.hub.log.Warn("board frame over the size cap", "file_id", c.room.fileID)
			}
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var msg inbound
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		c.handle(&msg)
	}
}

// handle applies one inbound message. Everything that mutates the scene is
// gated on the write permission, re-read on every message so a mid-session
// downgrade takes effect immediately.
func (c *client) handle(msg *inbound) {
	switch msg.Type {
	case msgPointer:
		// Cursors are relayed even for read-only viewers: seeing who is looking
		// is useful, and a pointer changes nothing in the document.
		c.room.broadcast(&outbound{
			Type:     msgPointer,
			From:     c.id,
			X:        msg.X,
			Y:        msg.Y,
			State:    msg.State,
			Selected: msg.Selected,
		}, c.id)

	case msgUpdate:
		if !c.writable() || len(msg.Elements) == 0 {
			return
		}
		if accepted := c.room.applyUpdate(msg.Elements); len(accepted) > 0 {
			c.room.broadcast(&outbound{Type: msgUpdate, From: c.id, Elements: accepted}, c.id)
		}

	case msgFiles:
		if !c.writable() || len(msg.Files) == 0 {
			return
		}
		if added := c.room.applyFiles(msg.Files); len(added) > 0 {
			c.room.broadcast(&outbound{Type: msgFiles, From: c.id, Files: added}, c.id)
		}

	case msgState:
		if !c.writable() {
			return
		}
		if c.room.applyBackground(msg.Background) {
			c.room.broadcast(&outbound{Type: msgState, From: c.id, Background: msg.Background}, c.id)
		}
	}
}

// colorFor derives a stable cursor colour from a peer id.
func colorFor(id string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return peerColors[int(h.Sum32())%len(peerColors)]
}

// marshal encodes an outbound message.
func marshal(msg *outbound) ([]byte, error) { return json.Marshal(msg) }

// isOverLimit reports whether err is the read-limit rejection, which is worth a
// log line (a peer tried to push a scene past the cap) unlike a plain
// disconnect.
func isOverLimit(err error) bool {
	var ce websocket.CloseError
	return errors.As(err, &ce) && ce.Code == websocket.StatusMessageTooBig
}
