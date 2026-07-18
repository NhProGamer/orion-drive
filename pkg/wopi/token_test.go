package wopi

import (
	"testing"
	"time"
)

func TestSignVerify(t *testing.T) {
	tok := NewToken("secret")
	s, err := tok.Sign(12, 3, true, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	f, u, w, err := tok.Verify(s)
	if err != nil || f != 12 || u != 3 || !w {
		t.Fatalf("verify: f=%d u=%d w=%v err=%v", f, u, w, err)
	}
}

func TestVerifyReadOnly(t *testing.T) {
	tok := NewToken("secret")
	s, _ := tok.Sign(1, 1, false, time.Hour)
	if _, _, w, err := tok.Verify(s); err != nil || w {
		t.Fatalf("expected view-only token, got w=%v err=%v", w, err)
	}
}

func TestVerifyRejects(t *testing.T) {
	tok := NewToken("secret")
	s, _ := tok.Sign(1, 1, true, time.Hour)
	if _, _, _, err := NewToken("other").Verify(s); err == nil {
		t.Error("accepted wrong secret")
	}
	if _, _, _, err := tok.Verify("garbage"); err == nil {
		t.Error("accepted garbage")
	}
	expired, _ := tok.Sign(1, 1, true, -time.Minute)
	if _, _, _, err := tok.Verify(expired); err == nil {
		t.Error("accepted expired")
	}
}
