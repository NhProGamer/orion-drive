// Package slaveauth signs and verifies requests exchanged between a master
// OrionDrive node and its storage slaves. Both sides share a secret key; the
// master signs each request (and each direct-download URL it hands to clients),
// and the slave verifies the signature before serving it.
//
// The signature covers the HTTP method, the request path, the query parameters
// (excluding the signature itself) and an expiry timestamp, so a signed URL is
// bound to one operation and stops working once it expires.
package slaveauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Query parameter names carrying the signature material.
const (
	ParamExpire = "expire"
	ParamSign   = "sign"
)

// Verification errors.
var (
	ErrMissing = errors.New("slaveauth: missing signature")
	ErrExpired = errors.New("slaveauth: signature expired")
	ErrInvalid = errors.New("slaveauth: invalid signature")
)

var b64 = base64.RawURLEncoding

// canonical builds the deterministic string that gets signed. Values are taken
// decoded (as url.Values yields them), so both ends agree regardless of how the
// query was percent-encoded on the wire.
func canonical(method, path string, q url.Values, expire int64) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		if k == ParamSign || k == ParamExpire {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(strings.ToUpper(method))
	b.WriteByte('\n')
	b.WriteString(path)
	b.WriteByte('\n')
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(v)
			b.WriteByte('&')
		}
	}
	b.WriteByte('\n')
	b.WriteString(strconv.FormatInt(expire, 10))
	return b.String()
}

func sign(secret, msg string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(msg))
	return b64.EncodeToString(h.Sum(nil))
}

// SignValues returns q with expire and sign parameters added, authorising a
// request with the given method and path for the duration ttl.
func SignValues(secret, method, path string, q url.Values, ttl time.Duration) url.Values {
	if q == nil {
		q = url.Values{}
	}
	expire := time.Now().Add(ttl).Unix()
	sig := sign(secret, canonical(method, path, q, expire))
	q.Set(ParamExpire, strconv.FormatInt(expire, 10))
	q.Set(ParamSign, sig)
	return q
}

// Verify checks the signature carried in q against method and path.
func Verify(secret, method, path string, q url.Values) error {
	sig := q.Get(ParamSign)
	if sig == "" {
		return ErrMissing
	}
	expire, err := strconv.ParseInt(q.Get(ParamExpire), 10, 64)
	if err != nil {
		return ErrInvalid
	}
	if time.Now().Unix() > expire {
		return ErrExpired
	}
	want := sign(secret, canonical(method, path, q, expire))
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return ErrInvalid
	}
	return nil
}
