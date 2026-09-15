package board

import (
	"context"
	"encoding/json"
	"maps"
	"sync"
	"time"
)

// room holds the authoritative scene of one open board plus its connected peers.
// Every mutation goes through mu; snapshots to storage are written by a
// dedicated goroutine (saveLoop) so a slow storage backend never stalls the
// relay.
type room struct {
	hub     *Hub
	fileID  uint
	ownerID uint

	mu         sync.Mutex
	elements   map[string]*element
	background string
	files      map[string]json.RawMessage
	peers      map[string]*client
	seq        uint64 // next insertion order for an index-less element
	dirty      bool
	retired    bool
	// entityID is the storage version this session has been overwriting. The
	// first snapshot creates it; later ones replace it, so an hour of drawing
	// leaves one entry in the file's history instead of hundreds.
	entityID uint

	notify chan struct{}
	done   chan struct{}
	closed sync.Once
}

// newRoom loads a board's stored scene into a fresh room.
func newRoom(ctx context.Context, h *Hub, fileID, ownerID uint) (*room, error) {
	data, err := h.store.Load(ctx, ownerID, fileID)
	if err != nil {
		return nil, err
	}
	elements, background, files, err := decodeScene(data)
	if err != nil {
		return nil, err
	}
	r := &room{
		hub:        h,
		fileID:     fileID,
		ownerID:    ownerID,
		elements:   elements,
		background: background,
		files:      files,
		peers:      map[string]*client{},
		seq:        uint64(len(elements)),
		notify:     make(chan struct{}, 1),
		done:       make(chan struct{}),
	}
	go r.saveLoop()
	return r, nil
}

// snapshot returns the current scene for a joining peer.
func (r *room) snapshot() *scene {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := make([]*element, 0, len(r.elements))
	for _, el := range r.elements {
		kept = append(kept, el)
	}
	sortElements(kept)
	raws := make([]json.RawMessage, 0, len(kept))
	for _, el := range kept {
		raws = append(raws, el.Raw)
	}
	return &scene{
		Elements:   raws,
		Background: r.background,
		Files:      maps.Clone(r.files),
	}
}

// peerList returns the current participants.
func (r *room) peerList() []*Peer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peerListLocked()
}

func (r *room) peerListLocked() []*Peer {
	out := make([]*Peer, 0, len(r.peers))
	for _, c := range r.peers {
		out = append(out, c.peer())
	}
	return out
}

// applyUpdate merges the elements a peer sent and returns those that actually
// changed the scene, which are the only ones worth relaying. Updates that lose
// the version comparison are dropped: the sender's own reconciliation will pull
// it back in line from the next broadcast it receives.
func (r *room) applyUpdate(raws []json.RawMessage) []json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	accepted := make([]json.RawMessage, 0, len(raws))
	for _, raw := range raws {
		el, err := parseElement(raw)
		if err != nil {
			continue
		}
		prev, exists := r.elements[el.id]
		if !el.supersedes(prev) {
			continue
		}
		// A brand-new element must not push the scene past the cap; an update to
		// an element already in the scene always may, so a peer can never be
		// locked out of deleting or fixing what it just drew.
		if !exists && len(r.elements) >= r.hub.opts.MaxElements {
			continue
		}
		if exists {
			el.seq = prev.seq
		} else {
			el.seq = r.seq
			r.seq++
		}
		r.elements[el.id] = el
		accepted = append(accepted, raw)
	}
	if len(accepted) > 0 {
		r.markDirtyLocked()
	}
	return accepted
}

// applyFiles records binary assets (pasted images) a peer added. Existing ids
// are never overwritten: an Excalidraw file id is a content hash, so a second
// upload under the same id carries the same bytes.
func (r *room) applyFiles(files map[string]json.RawMessage) map[string]json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	added := map[string]json.RawMessage{}
	for id, raw := range files {
		if id == "" || len(r.files) >= r.hub.opts.MaxFiles {
			continue
		}
		if _, ok := r.files[id]; ok {
			continue
		}
		r.files[id] = raw
		added[id] = raw
	}
	if len(added) > 0 {
		r.markDirtyLocked()
	}
	return added
}

// applyBackground records a canvas background change.
func (r *room) applyBackground(color string) bool {
	if color == "" || len(color) > 32 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.background == color {
		return false
	}
	r.background = color
	r.markDirtyLocked()
	return true
}

// markDirtyLocked flags unsaved changes and wakes the saver. Called with mu held.
func (r *room) markDirtyLocked() {
	r.dirty = true
	select {
	case r.notify <- struct{}{}:
	default: // already pending
	}
}

// broadcast sends msg to every peer except `except` (use "" to include all).
func (r *room) broadcast(msg *outbound, except string) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	r.mu.Lock()
	targets := make([]*client, 0, len(r.peers))
	for id, c := range r.peers {
		if id == except {
			continue
		}
		targets = append(targets, c)
	}
	r.mu.Unlock()
	for _, c := range targets {
		c.send(data)
	}
}

// saveLoop snapshots the scene at most once per SaveInterval while edits keep
// arriving, and once more when the room retires. Bounding the rate this way
// caps write amplification without ever letting the last edit go unsaved.
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

// flush writes the scene to storage when it has unsaved changes.
func (r *room) flush() {
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return
	}
	data, err := encodeScene(r.elements, r.background, r.files)
	if err != nil {
		r.mu.Unlock()
		r.hub.log.Error("board snapshot: encode failed", "file_id", r.fileID, "error", err)
		return
	}
	// Clear the flag before writing so edits landing during the write mark the
	// scene dirty again rather than being swallowed by this snapshot.
	r.dirty = false
	entityID := r.entityID
	r.mu.Unlock()

	if int64(len(data)) > r.hub.opts.MaxSceneBytes {
		r.hub.log.Warn("board snapshot: scene over the size cap, not saved",
			"file_id", r.fileID, "bytes", len(data), "cap", r.hub.opts.MaxSceneBytes)
		r.broadcast(&outbound{Type: msgError, Message: "scene is too large to save"}, "")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	newID, err := r.hub.store.Save(ctx, r.ownerID, r.fileID, entityID, data)
	if err != nil {
		// Put the flag back so the next tick retries instead of losing the edits.
		r.mu.Lock()
		r.dirty = true
		r.mu.Unlock()
		r.hub.log.Error("board snapshot failed", "file_id", r.fileID, "error", err)
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

// saveTimeout bounds a single snapshot write, so a wedged storage backend cannot
// hold the saver goroutine forever.
const saveTimeout = 30 * time.Second
