// Package wopi implements the pieces of the WOPI protocol OrionDrive needs to
// host files in an online Office editor (Collabora Online, OnlyOffice Docs).
// Access to a file through WOPI is authorised by a short-lived signed token that
// binds a file to the user that opened it.
package wopi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

// ErrInvalidToken is returned when a WOPI access token fails verification.
var ErrInvalidToken = errors.New("invalid or expired WOPI token")

var b64 = base64.RawURLEncoding

type payload struct {
	FileID   uint   `json:"f"`
	UID      uint   `json:"u"`
	CanWrite bool   `json:"w,omitempty"`
	Exp      int64  `json:"e"`
	Share    string `json:"s,omitempty"` // originating share token (empty for owner sessions)
	// Editor identity shown in the online editor, distinct from UID (the storage
	// owner). For the owner it equals their id; for a share visitor it is a unique
	// per-session id + display name, so co-editors are not all shown as the owner.
	EditorID   string `json:"i,omitempty"`
	EditorName string `json:"n,omitempty"`
}

// Token binds a file and user, signed with the server secret.
type Token struct{ secret []byte }

// NewToken builds a token signer/verifier from the session secret.
func NewToken(secret string) *Token { return &Token{secret: []byte(secret)} }

// Sign issues an access token for (fileID, uid) valid for ttl. canWrite records
// whether the holder may save changes back (false = view-only). shareToken is
// the originating public share (empty for an owner editing their own file); it
// lets the host revalidate the share on each call so revocation takes effect.
// editorID/editorName carry the identity shown in the online editor — for a
// share visitor this is a distinct per-session identity, not the owner's, so
// co-editors are no longer all labelled with the file owner's name.
func (t *Token) Sign(fileID, uid uint, canWrite bool, shareToken, editorID, editorName string, ttl time.Duration) (string, error) {
	body, err := json.Marshal(payload{FileID: fileID, UID: uid, CanWrite: canWrite, Exp: time.Now().Add(ttl).Unix(), Share: shareToken, EditorID: editorID, EditorName: editorName})
	if err != nil {
		return "", err
	}
	enc := b64.EncodeToString(body)
	return enc + "." + b64.EncodeToString(t.mac([]byte(enc))), nil
}

// Verify checks a token and returns the bound file, user, write permission,
// originating share token (empty for an owner session), and the editor identity
// (id + display name) to show in the online editor.
func (t *Token) Verify(token string) (fileID, uid uint, canWrite bool, shareToken, editorID, editorName string, err error) {
	var body, sig string
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			body, sig = token[:i], token[i+1:]
			break
		}
	}
	if body == "" || sig == "" {
		return 0, 0, false, "", "", "", ErrInvalidToken
	}
	got, err := b64.DecodeString(sig)
	if err != nil || !hmac.Equal(t.mac([]byte(body)), got) {
		return 0, 0, false, "", "", "", ErrInvalidToken
	}
	raw, err := b64.DecodeString(body)
	if err != nil {
		return 0, 0, false, "", "", "", ErrInvalidToken
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return 0, 0, false, "", "", "", ErrInvalidToken
	}
	if time.Now().Unix() > p.Exp {
		return 0, 0, false, "", "", "", ErrInvalidToken
	}
	return p.FileID, p.UID, p.CanWrite, p.Share, p.EditorID, p.EditorName, nil
}

func (t *Token) mac(body []byte) []byte {
	h := hmac.New(sha256.New, t.secret)
	h.Write(body)
	return h.Sum(nil)
}
