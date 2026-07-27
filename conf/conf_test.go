package conf

import "testing"

func TestWOPITokenSecret(t *testing.T) {
	if got := (WOPI{}).TokenSecret("session"); got != "session" {
		t.Fatalf("empty WOPI secret should fall back to session secret, got %q", got)
	}
	if got := (WOPI{Secret: "dedicated"}).TokenSecret("session"); got != "dedicated" {
		t.Fatalf("dedicated WOPI secret should win, got %q", got)
	}
}
