package slaveauth

import (
	"net/url"
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	q := url.Values{}
	q.Set("path", "u1/abc.txt")
	signed := SignValues("secret", "GET", "/api/v1/slave/content", q, time.Hour)

	if err := Verify("secret", "GET", "/api/v1/slave/content", signed); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	q := url.Values{}
	q.Set("path", "u1/abc.txt")
	signed := SignValues("secret", "GET", "/api/v1/slave/content", q, time.Hour)

	if err := Verify("other-secret", "GET", "/api/v1/slave/content", clone(signed)); err == nil {
		t.Error("accepted wrong secret")
	}
	if err := Verify("secret", "POST", "/api/v1/slave/content", clone(signed)); err == nil {
		t.Error("accepted wrong method")
	}
	if err := Verify("secret", "GET", "/api/v1/slave/upload", clone(signed)); err == nil {
		t.Error("accepted wrong path")
	}
	tampered := clone(signed)
	tampered.Set("path", "u1/other.txt")
	if err := Verify("secret", "GET", "/api/v1/slave/content", tampered); err == nil {
		t.Error("accepted altered param")
	}
	noSign := clone(signed)
	noSign.Del(ParamSign)
	if err := Verify("secret", "GET", "/api/v1/slave/content", noSign); err == nil {
		t.Error("accepted missing signature")
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	q := url.Values{}
	q.Set("path", "x")
	signed := SignValues("secret", "GET", "/p", q, -time.Minute)
	if err := Verify("secret", "GET", "/p", signed); err != ErrExpired {
		t.Fatalf("want ErrExpired, got %v", err)
	}
}

func clone(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		for _, s := range vs {
			out.Add(k, s)
		}
	}
	return out
}
