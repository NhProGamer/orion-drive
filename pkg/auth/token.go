// Package auth implements OIDC login and signed session tokens.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidToken is returned when a session token fails verification.
var ErrInvalidToken = errors.New("invalid or expired token")

// tokenPayload is the signed session content.
type tokenPayload struct {
	UID uint  `json:"uid"`
	Exp int64 `json:"exp"`
}

// Signer issues and verifies compact HMAC-signed session tokens
// (payload.signature), a self-contained JWT-lite keyed by the session secret.
type Signer struct {
	secret []byte
}

// NewSigner builds a Signer from the configured session secret.
func NewSigner(secret string) *Signer {
	return &Signer{secret: []byte(secret)}
}

var b64 = base64.RawURLEncoding

// Sign returns a token for uid valid for ttl.
func (s *Signer) Sign(uid uint, ttl time.Duration) (string, error) {
	payload, err := json.Marshal(tokenPayload{UID: uid, Exp: time.Now().Add(ttl).Unix()})
	if err != nil {
		return "", err
	}
	body := b64.EncodeToString(payload)
	return body + "." + b64.EncodeToString(s.mac([]byte(body))), nil
}

// Verify checks the signature and expiry and returns the user ID.
func (s *Signer) Verify(token string) (uint, error) {
	var body, sig string
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			body, sig = token[:i], token[i+1:]
			break
		}
	}
	if body == "" || sig == "" {
		return 0, ErrInvalidToken
	}
	want := s.mac([]byte(body))
	got, err := b64.DecodeString(sig)
	if err != nil || !hmac.Equal(want, got) {
		return 0, ErrInvalidToken
	}
	raw, err := b64.DecodeString(body)
	if err != nil {
		return 0, ErrInvalidToken
	}
	var p tokenPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return 0, ErrInvalidToken
	}
	if time.Now().Unix() > p.Exp {
		return 0, fmt.Errorf("%w: expired", ErrInvalidToken)
	}
	return p.UID, nil
}

func (s *Signer) mac(body []byte) []byte {
	h := hmac.New(sha256.New, s.secret)
	h.Write(body)
	return h.Sum(nil)
}
