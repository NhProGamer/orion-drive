// Package livedoc implements the relay behind collaborative Markdown editing.
//
// Unlike the whiteboard relay, this server does not understand the documents it
// carries. Rich text cannot be merged by last-write-wins the way whiteboard
// elements can — two people typing in the same paragraph need character-level
// convergence — so the clients run a CRDT (Yjs) and the server only forwards
// their opaque update frames between peers. What it does own is the file: it
// seeds the first editor from the stored Markdown, and writes the Markdown its
// elected writer sends back, snapshot-throttled, as a single version per
// editing session.
package livedoc

// Control-message types. They travel as JSON text frames, while the CRDT's own
// updates travel as binary frames — keeping both on one socket means one
// authorisation, one revalidation loop and one peer list to reason about.
const (
	// Server → client.
	msgInit     = "init"     // seed + permission + peer list, sent once on join
	msgPeers    = "peers"    // peer list changed
	msgWriter   = "writer"   // this peer became (or stopped being) the writer
	msgSaved    = "saved"    // the document was snapshotted to storage
	msgReadOnly = "readonly" // write permission changed mid-session
	msgError    = "error"    // fatal: the connection closes right after

	// Client → server.
	msgSave = "save" // Markdown serialised by the writer
)

// inbound is a control message received from a peer.
type inbound struct {
	Type string `json:"t"`
	Text string `json:"text,omitempty"`
	// Synced reports that the sender's document reflects either the seed it was
	// given or a completed sync with the other peers. It guards the one
	// destructive case: an empty document is only allowed to overwrite a
	// non-empty file when the sender knows its own state is real, not merely
	// not-yet-loaded.
	Synced bool `json:"synced,omitempty"`
}

// outbound is a control message sent to a peer.
type outbound struct {
	Type string `json:"t"`

	Self     string  `json:"self,omitempty"`
	CanWrite *bool   `json:"can_write,omitempty"`
	Writer   *bool   `json:"writer,omitempty"`
	Peers    []*Peer `json:"peers,omitempty"`
	// Seed carries the stored Markdown, and is sent only to the peer that opened
	// an empty room. Everyone else builds the document by syncing with the peers
	// already in it, so the file is read once per session.
	Seed    *string `json:"seed,omitempty"`
	Message string  `json:"message,omitempty"`
}

// Peer is one participant as advertised to the others. Only the display name
// and colour are exposed: anonymous share visitors sit in the same room as
// signed-in users, and neither should learn the other's identity.
type Peer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Write bool   `json:"write"`
}
