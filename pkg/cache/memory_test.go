package cache

import (
	"testing"
	"time"
)

func TestMemoryRoundtrip(t *testing.T) {
	c := NewMemory()

	if err := c.Set("k", []byte("v"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, ok := c.Get("k"); !ok || string(v) != "v" {
		t.Fatalf("get after set: %q %v", v, ok)
	}

	// Overwrite.
	_ = c.Set("k", []byte("v2"), 0)
	if v, _ := c.Get("k"); string(v) != "v2" {
		t.Fatalf("overwrite: %q", v)
	}

	// Delete.
	_ = c.Delete("k")
	if _, ok := c.Get("k"); ok {
		t.Fatalf("key present after delete")
	}

	// Missing key.
	if _, ok := c.Get("absent"); ok {
		t.Fatalf("absent key reported present")
	}
}

func TestMemoryExpiry(t *testing.T) {
	c := NewMemory()
	_ = c.Set("e", []byte("x"), time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	if _, ok := c.Get("e"); ok {
		t.Fatalf("expired key still present")
	}
	// A zero TTL never expires.
	_ = c.Set("forever", []byte("y"), 0)
	time.Sleep(5 * time.Millisecond)
	if _, ok := c.Get("forever"); !ok {
		t.Fatalf("no-expiry key vanished")
	}
}
