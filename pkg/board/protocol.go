package board

import "encoding/json"

// Message types exchanged over the board WebSocket. The wire format is plain
// JSON: the relay has to read element versions to merge them, so a binary or
// end-to-end encrypted payload (as upstream Excalidraw uses between browsers)
// would defeat the server-side authoritative scene this feature is built on —
// which is what lets a late joiner and the saved file stay in sync.
const (
	// Server → client.
	msgInit     = "init"     // full scene + peer list, sent once on join
	msgPeers    = "peers"    // peer list changed
	msgSaved    = "saved"    // scene was snapshotted to storage
	msgError    = "error"    // fatal: the connection closes right after
	msgReadOnly = "readonly" // write permission was revoked mid-session

	// Both directions.
	msgUpdate  = "update"  // element upserts
	msgPointer = "pointer" // cursor position / selection
	msgFiles   = "files"   // binary assets (pasted or dropped images)
	msgState   = "state"   // canvas-level state (background colour)
)

// inbound is a message received from a peer.
type inbound struct {
	Type     string                     `json:"t"`
	Elements []json.RawMessage          `json:"elements,omitempty"`
	Files    map[string]json.RawMessage `json:"files,omitempty"`

	// Pointer payload, relayed verbatim to the other peers.
	X        float64  `json:"x,omitempty"`
	Y        float64  `json:"y,omitempty"`
	State    string   `json:"state,omitempty"`
	Selected []string `json:"selected,omitempty"`

	// Canvas state.
	Background string `json:"background,omitempty"`
}

// outbound is a message sent to a peer. Fields are omitted when empty so a
// pointer frame — by far the most frequent message — stays small.
type outbound struct {
	Type string `json:"t"`
	From string `json:"from,omitempty"`

	// Join payload.
	Self     string  `json:"self,omitempty"`
	CanWrite *bool   `json:"can_write,omitempty"`
	Scene    *scene  `json:"scene,omitempty"`
	Peers    []*Peer `json:"peers,omitempty"`

	Elements []json.RawMessage          `json:"elements,omitempty"`
	Files    map[string]json.RawMessage `json:"files,omitempty"`

	X        float64  `json:"x,omitempty"`
	Y        float64  `json:"y,omitempty"`
	State    string   `json:"state,omitempty"`
	Selected []string `json:"selected,omitempty"`

	Background string `json:"background,omitempty"`
	Message    string `json:"message,omitempty"`
}

// scene is the full board state a joining peer receives.
type scene struct {
	Elements   []json.RawMessage          `json:"elements"`
	Background string                     `json:"background"`
	Files      map[string]json.RawMessage `json:"files"`
}

// Peer is one participant, as advertised to the others. Only the display name
// and colour are exposed — never the e-mail or user id of a signed-in editor,
// since anonymous share visitors sit in the same room.
type Peer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Write bool   `json:"write"`
}
