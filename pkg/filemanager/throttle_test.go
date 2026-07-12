package filemanager

import (
	"bytes"
	"io"
	"testing"
	"time"
)

// memRSC is an in-memory ReadSeekCloser for tests.
type memRSC struct{ *bytes.Reader }

func (memRSC) Close() error { return nil }

func TestThrottledReaderPreservesContent(t *testing.T) {
	data := bytes.Repeat([]byte("orion"), 1000) // 5000 bytes
	r := newThrottledReader(memRSC{bytes.NewReader(data)}, 1<<20)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("throttled read corrupted content")
	}
}

func TestThrottledReaderLimitsRate(t *testing.T) {
	// Data larger than the burst so the limiter must actually wait.
	size := throttleBurst + 200<<10 // burst + 200 KiB
	data := bytes.Repeat([]byte{'x'}, size)
	limit := int64(throttleBurst) // ~256 KiB/s → the 200 KiB past the burst takes ~0.8s

	start := time.Now()
	r := newThrottledReader(memRSC{bytes.NewReader(data)}, limit)
	if _, err := io.ReadAll(r); err != nil {
		t.Fatalf("read: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 400*time.Millisecond {
		t.Fatalf("expected throttling to slow the read, took only %v", elapsed)
	}
}

func TestThrottledReaderNoLimitIsPassthrough(t *testing.T) {
	data := []byte("hello")
	r := newThrottledReader(memRSC{bytes.NewReader(data)}, 0)
	if _, ok := r.(memRSC); !ok {
		t.Fatal("expected zero limit to return the reader unwrapped")
	}
}
