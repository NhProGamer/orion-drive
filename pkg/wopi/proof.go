package wopi

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"math/big"
	"strings"
)

// proofKeyXML is the <proof-key> element of a discovery document. Servers give
// the public key as a base64 big-endian modulus and exponent, plus the previous
// key (during rotation) under the old* attributes.
type proofKeyXML struct {
	Modulus     string `xml:"modulus,attr"`
	Exponent    string `xml:"exponent,attr"`
	OldModulus  string `xml:"oldmodulus,attr"`
	OldExponent string `xml:"oldexponent,attr"`
}

// ProofKeys holds a document server's current and previous WOPI proof keys.
type ProofKeys struct {
	Current *rsa.PublicKey
	Old     *rsa.PublicKey
}

func parseProofKeys(px *proofKeyXML) *ProofKeys {
	if px == nil {
		return nil
	}
	pk := &ProofKeys{
		Current: rsaKey(px.Modulus, px.Exponent),
		Old:     rsaKey(px.OldModulus, px.OldExponent),
	}
	if pk.Current == nil && pk.Old == nil {
		return nil
	}
	return pk
}

func rsaKey(modulusB64, exponentB64 string) *rsa.PublicKey {
	mod := decodeBigEndian(modulusB64)
	exp := decodeBigEndian(exponentB64)
	if mod == nil || exp == nil || !exp.IsInt64() {
		return nil
	}
	return &rsa.PublicKey{N: mod, E: int(exp.Int64())}
}

func decodeBigEndian(s string) *big.Int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) == 0 {
		return nil
	}
	return new(big.Int).SetBytes(raw)
}

// expected builds the byte string a WOPI proof signs: the access token, the
// upper-cased request URL, and the timestamp, each length-prefixed (big-endian).
func expected(accessToken, requestURL string, timestamp int64) []byte {
	tok := []byte(accessToken)
	u := []byte(strings.ToUpper(requestURL))
	buf := make([]byte, 0, 4+len(tok)+4+len(u)+4+8)
	put32 := func(n int) { var b [4]byte; binary.BigEndian.PutUint32(b[:], uint32(n)); buf = append(buf, b[:]...) }
	put32(len(tok))
	buf = append(buf, tok...)
	put32(len(u))
	buf = append(buf, u...)
	put32(8)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(timestamp))
	buf = append(buf, ts[:]...)
	return buf
}

// Verify checks a WOPI request's proof headers. Per the protocol a request is
// authentic if any of these hold: proof signed by the current key, proofOld
// signed by the current key, or proof signed by the old key.
func (pk *ProofKeys) Verify(accessToken, requestURL string, timestamp int64, proof, proofOld string) bool {
	if pk == nil {
		return false
	}
	want := expected(accessToken, requestURL, timestamp)
	sig := decode(proof)
	sigOld := decode(proofOld)
	return verify(pk.Current, want, sig) ||
		verify(pk.Current, want, sigOld) ||
		verify(pk.Old, want, sig)
}

func decode(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return b
}

func verify(key *rsa.PublicKey, message, sig []byte) bool {
	if key == nil || len(sig) == 0 {
		return false
	}
	sum := sha256.Sum256(message)
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig) == nil
}
