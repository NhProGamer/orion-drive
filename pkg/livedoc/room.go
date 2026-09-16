package livedoc

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// room holds the peers editing one document. The document itself lives in the
// clients' CRDT replicas, not here; the room's own state is the peer list, who
// is currently elected to write, and the Markdown waiting to be snapshotted.
type room struct {
	hub     *Hub
	fileID  uint
	ownerID uint

	mu      sync.Mutex
	peers   map[string]*client
	order   []string // join order, so the writer election is deterministic
	seeded  bool     // a peer has already been handed the stored Markdown
	retired bool

	// pending is the newest Markdown the writer sent and that is not on disk yet.
	pending  *string
	entityID uint // storage version this session keeps overwriting

	notify chan struct{}
	done   chan struct{}
	closed sync.Once
}

func newRoom(h *Hub, fileID, ownerID uint) *room {
	r := &room{
		hub:     h,
		fileID:  fileID,
		ownerID: ownerID,
		peers:   map[string]*client{},
		notify:  make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	go r.saveLoop()
	return r
}

// add registers a peer and reports whether it must seed the document from the
// stored file, which is true only for the peer that finds the room empty.
func (r *room) add(c *client) (seed bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.peers) >= r.hub.opts.MaxPeers {
		return false, ErrRoomFull
	}
	r.peers[c.id] = c
	r.order = append(r.order, c.id)
	if !r.seeded {
		r.seeded = true
		seed = true
	}
	r.electLocked()
	return seed, nil
}

// remove drops a peer, re-electing the writer if it was the one leaving.
func (r *room) remove(c *client) {
	r.mu.Lock()
	if _, ok := r.peers[c.id]; !ok {
		r.mu.Unlock()
		return
	}
	delete(r.peers, c.id)
	for i, id := range r.order {
		if id == c.id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.electLocked()
	peers := r.peerListLocked()
	r.mu.Unlock()
	c.stop()
	r.broadcast(&outbound{Type: msgPeers, Peers: peers}, "")
}

// electLocked makes the longest-connected writable peer the writer, and tells
// any peer whose role changed. Exactly one writer means the file is never
// written by two peers racing on the same version. Called with mu held.
func (r *room) electLocked() {
	var chosen *client
	for _, id := range r.order {
		if c := r.peers[id]; c != nil && c.writable() {
			chosen = c
			break
		}
	}
	for _, c := range r.peers {
		want := c == chosen
		if c.setWriter(want) {
			c.sendMsg(&outbound{Type: msgWriter, Writer: &want})
		}
	}
}

// reelect re-runs the election, used when a peer's permission changes.
func (r *room) reelect() {
	r.mu.Lock()
	r.electLocked()
	r.mu.Unlock()
}

func (r *room) peerList() []*Peer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peerListLocked()
}

func (r *room) peerListLocked() []*Peer {
	out := make([]*Peer, 0, len(r.peers))
	for _, id := range r.order {
		if c := r.peers[id]; c != nil {
			out = append(out, c.peer())
		}
	}
	return out
}

// relay forwards an opaque CRDT frame to every peer but its sender.
func (r *room) relay(frame []byte, from string) {
	r.mu.Lock()
	targets := make([]*client, 0, len(r.peers))
	for id, c := range r.peers {
		if id != from {
			targets = append(targets, c)
		}
	}
	r.mu.Unlock()
	for _, c := range targets {
		c.sendBinary(frame)
	}
}

// broadcast sends a control message to every peer but `except` ("" for all).
func (r *room) broadcast(msg *outbound, except string) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	r.mu.Lock()
	targets := make([]*client, 0, len(r.peers))
	for id, c := range r.peers {
		if id != except {
			targets = append(targets, c)
		}
	}
	r.mu.Unlock()
	for _, c := range targets {
		c.send(data)
	}
}

// stage records Markdown to be written, replacing anything not yet flushed.
func (r *room) stage(text string) {
	r.mu.Lock()
	r.pending = &text
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default: // a flush is already scheduled
	}
}

// saveLoop writes at most one snapshot per SaveInterval while edits keep
// arriving, and a last one when the room retires — so a long editing session
// costs a bounded number of writes without ever dropping the final keystroke.
func (r *room) saveLoop() {
	timer := time.NewTimer(r.hub.opts.SaveInterval)
	if !timer.Stop() {
		<-timer.C
	}
	armed := false
	for {
		select {
		case <-r.notify:
			if !armed {
				timer.Reset(r.hub.opts.SaveInterval)
				armed = true
			}
		case <-timer.C:
			armed = false
			r.flush()
		case <-r.done:
			if armed && !timer.Stop() {
				<-timer.C
			}
			r.flush()
			return
		}
	}
}

// flush writes the staged Markdown to storage.
func (r *room) flush() {
	r.mu.Lock()
	if r.pending == nil {
		r.mu.Unlock()
		return
	}
	text := *r.pending
	// Cleared before the write so edits landing during it stage a new snapshot
	// instead of being swallowed by this one.
	r.pending = nil
	entityID := r.entityID
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	newID, err := r.hub.store.Save(ctx, r.ownerID, r.fileID, entityID, []byte(text))
	if err != nil {
		// Put it back so the next tick retries rather than losing the edits.
		r.mu.Lock()
		if r.pending == nil {
			r.pending = &text
		}
		r.mu.Unlock()
		r.hub.log.Error("document snapshot failed", "file_id", r.fileID, "error", err)
		return
	}
	r.mu.Lock()
	r.entityID = newID
	r.mu.Unlock()
	r.broadcast(&outbound{Type: msgSaved}, "")
}

// close retires the room: the saver writes one last snapshot and exits.
func (r *room) close() {
	r.closed.Do(func() { close(r.done) })
}

// saveTimeout bounds a single snapshot write.
const saveTimeout = 30 * time.Second
