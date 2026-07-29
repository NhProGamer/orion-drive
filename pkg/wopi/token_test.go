package wopi

import (
	"testing"
	"time"
)

func TestSignVerify(t *testing.T) {
	tok := NewToken("secret")
	s, err := tok.Sign(12, 3, true, "shr", "g7", "Alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	f, u, w, sh, ei, en, err := tok.Verify(s)
	if err != nil || f != 12 || u != 3 || !w || sh != "shr" || ei != "g7" || en != "Alice" {
		t.Fatalf("verify: f=%d u=%d w=%v sh=%q ei=%q en=%q err=%v", f, u, w, sh, ei, en, err)
	}
}

func TestVerifyReadOnly(t *testing.T) {
	tok := NewToken("secret")
	s, _ := tok.Sign(1, 1, false, "", "", "", time.Hour)
	if _, _, w, sh, _, _, err := tok.Verify(s); err != nil || w || sh != "" {
		t.Fatalf("expected view-only owner token, got w=%v sh=%q err=%v", w, sh, err)
	}
}

func TestVerifyRejects(t *testing.T) {
	tok := NewToken("secret")
	s, _ := tok.Sign(1, 1, true, "", "", "", time.Hour)
	if _, _, _, _, _, _, err := NewToken("other").Verify(s); err == nil {
		t.Error("accepted wrong secret")
	}
	if _, _, _, _, _, _, err := tok.Verify("garbage"); err == nil {
		t.Error("accepted garbage")
	}
	expired, _ := tok.Sign(1, 1, true, "", "", "", -time.Minute)
	if _, _, _, _, _, _, err := tok.Verify(expired); err == nil {
		t.Error("accepted expired")
	}
}
