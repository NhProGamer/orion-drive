package wopi

import (
	"testing"
	"time"
)

func TestSignVerify(t *testing.T) {
	tok := NewToken("secret")
	s, err := tok.Sign(12, 3, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	f, u, err := tok.Verify(s)
	if err != nil || f != 12 || u != 3 {
		t.Fatalf("verify: f=%d u=%d err=%v", f, u, err)
	}
}

func TestVerifyRejects(t *testing.T) {
	tok := NewToken("secret")
	s, _ := tok.Sign(1, 1, time.Hour)
	if _, _, err := NewToken("other").Verify(s); err == nil {
		t.Error("accepted wrong secret")
	}
	if _, _, err := tok.Verify("garbage"); err == nil {
		t.Error("accepted garbage")
	}
	expired, _ := tok.Sign(1, 1, -time.Minute)
	if _, _, err := tok.Verify(expired); err == nil {
		t.Error("accepted expired")
	}
}
