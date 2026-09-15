// Package board implements the real-time relay behind the collaborative
// whiteboard: it keeps the authoritative scene of every open .excalidraw file in
// memory, merges the element updates peers send over a WebSocket and snapshots
// the result back to storage.
//
// The merge rule is Excalidraw's own: an element wins on the higher version and,
// at equal versions, on the lower versionNonce. That makes the relay a plain
// last-write-wins store with deterministic tie-breaking, so every peer converges
// on the same scene without the server understanding element semantics.
package board

import (
	"encoding/json"
	"fmt"
	"sort"
)

// SceneFormat is the `type` discriminator Excalidraw writes in a .excalidraw
// document, and the only one this package accepts.
const SceneFormat = "excalidraw"

// sceneVersion is the document schema version Excalidraw currently emits.
const sceneVersion = 2

// sceneSource identifies the writer in saved documents.
const sceneSource = "OrionDrive"

// defaultBackground matches Excalidraw's own default canvas colour.
const defaultBackground = "#ffffff"

// document is the on-disk shape of a .excalidraw file. Only the fields the relay
// owns are typed; everything else in appState is deliberately dropped, as it is
// per-viewer UI state (zoom, scroll, current tool) rather than scene content.
type document struct {
	Type     string                     `json:"type"`
	Version  int                        `json:"version"`
	Source   string                     `json:"source"`
	Elements []json.RawMessage          `json:"elements"`
	AppState docAppState                `json:"appState"`
	Files    map[string]json.RawMessage `json:"files"`
}

type docAppState struct {
	ViewBackgroundColor string `json:"viewBackgroundColor"`
	// GridSize is echoed back untouched so toggling the grid survives a save.
	GridSize json.RawMessage `json:"gridSize,omitempty"`
}

// element is the subset of an Excalidraw element the relay needs to merge and
// order it. Raw carries the untouched element so no property is ever lost.
type element struct {
	Raw json.RawMessage

	id      string
	version int64
	nonce   int64
	index   string
	deleted bool
	// seq orders elements that carry no fractional index (documents written by
	// pre-0.17 Excalidraw), preserving the order they were first seen in.
	seq uint64
}

// elementHeader is the projection parsed out of an element to merge it.
type elementHeader struct {
	ID           string `json:"id"`
	Version      int64  `json:"version"`
	VersionNonce int64  `json:"versionNonce"`
	Index        string `json:"index"`
	IsDeleted    bool   `json:"isDeleted"`
}

// parseElement projects the merge-relevant header out of a raw element. An
// element without an id cannot be addressed, merged or deleted, so it is
// rejected rather than stored under an empty key.
func parseElement(raw json.RawMessage) (*element, error) {
	var h elementHeader
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("malformed element: %w", err)
	}
	if h.ID == "" {
		return nil, fmt.Errorf("element without an id")
	}
	return &element{
		Raw:     raw,
		id:      h.ID,
		version: h.Version,
		nonce:   h.VersionNonce,
		index:   h.Index,
		deleted: h.IsDeleted,
	}, nil
}

// supersedes reports whether e should replace prev, using Excalidraw's
// reconciliation rule: the higher version wins and, when two peers bumped the
// same element to the same version concurrently, the lower versionNonce does.
func (e *element) supersedes(prev *element) bool {
	if prev == nil {
		return true
	}
	if e.version != prev.version {
		return e.version > prev.version
	}
	if e.nonce != prev.nonce {
		return e.nonce < prev.nonce
	}
	// Identical version and nonce: same element, nothing to apply.
	return false
}

// decodeScene parses a stored .excalidraw document. An empty (zero-byte) file is
// a freshly created board and decodes to an empty scene.
func decodeScene(data []byte) (elements map[string]*element, background string, files map[string]json.RawMessage, err error) {
	elements = map[string]*element{}
	files = map[string]json.RawMessage{}
	background = defaultBackground
	if len(trimSpace(data)) == 0 {
		return elements, background, files, nil
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, "", nil, fmt.Errorf("parse scene: %w", err)
	}
	if doc.Type != "" && doc.Type != SceneFormat {
		return nil, "", nil, fmt.Errorf("not an Excalidraw document (type %q)", doc.Type)
	}
	for i, raw := range doc.Elements {
		el, err := parseElement(raw)
		if err != nil {
			// A single unreadable element must not make the whole board
			// unopenable; skip it and keep the rest of the scene.
			continue
		}
		el.seq = uint64(i)
		elements[el.id] = el
	}
	if doc.AppState.ViewBackgroundColor != "" {
		background = doc.AppState.ViewBackgroundColor
	}
	for k, v := range doc.Files {
		files[k] = v
	}
	return elements, background, files, nil
}

// encodeScene serialises the live scene back into a .excalidraw document.
// Tombstones (isDeleted elements) are dropped: they only exist to propagate
// deletions to peers during a session, and Excalidraw omits them when saving.
func encodeScene(elements map[string]*element, background string, files map[string]json.RawMessage) ([]byte, error) {
	kept := make([]*element, 0, len(elements))
	for _, el := range elements {
		if el.deleted {
			continue
		}
		kept = append(kept, el)
	}
	sortElements(kept)
	raws := make([]json.RawMessage, 0, len(kept))
	for _, el := range kept {
		raws = append(raws, el.Raw)
	}
	if background == "" {
		background = defaultBackground
	}
	if files == nil {
		files = map[string]json.RawMessage{}
	}
	return json.Marshal(document{
		Type:     SceneFormat,
		Version:  sceneVersion,
		Source:   sceneSource,
		Elements: raws,
		AppState: docAppState{ViewBackgroundColor: background},
		Files:    files,
	})
}

// sortElements restores z-order. Excalidraw encodes it in the fractional `index`
// string, which sorts lexicographically; elements from older documents carry no
// index and fall back to the order they were first seen in.
func sortElements(els []*element) {
	sort.Slice(els, func(i, j int) bool {
		if els[i].index != els[j].index {
			return els[i].index < els[j].index
		}
		return els[i].seq < els[j].seq
	})
}

// trimSpace reports the input without leading/trailing ASCII whitespace, used
// only to tell an empty file from a real document.
func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && isSpace(b[i]) {
		i++
	}
	for j > i && isSpace(b[j-1]) {
		j--
	}
	return b[i:j]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// EmptyScene returns the document written for a newly created board.
func EmptyScene() []byte {
	data, _ := encodeScene(map[string]*element{}, defaultBackground, nil)
	return data
}
