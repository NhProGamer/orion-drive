package driver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

// fetchCounter serves an in-memory object and records how many requests the
// reader made — the whole point of the range reader is to keep that number
// low, so the tests assert on it.
type fetchCounter struct {
	data  []byte
	calls int
	offs  []int64
}

func (f *fetchCounter) fetch(_ context.Context, off int64) (io.ReadCloser, int64, error) {
	f.calls++
	f.offs = append(f.offs, off)
	if off > int64(len(f.data)) {
		return nil, 0, errors.New("range beyond the object")
	}
	return io.NopCloser(bytes.NewReader(f.data[off:])), int64(len(f.data)), nil
}

func body(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte('a' + i%26)
	}
	return out
}

func TestRangeReaderStreamsInOneRequest(t *testing.T) {
	src := &fetchCounter{data: body(300 << 10)}
	r, err := NewRangeReader(context.Background(), src.fetch)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, src.data) {
		t.Fatalf("read %d bytes, want %d identical", len(got), len(src.data))
	}
	if src.calls != 1 {
		t.Fatalf("%d requests for a straight read, want 1", src.calls)
	}
}

func TestRangeReaderSeeksBackwardWithANewRequest(t *testing.T) {
	src := &fetchCounter{data: body(1000)}
	r, err := NewRangeReader(context.Background(), src.fetch)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	head := make([]byte, 10)
	if _, err := io.ReadFull(r, head); err != nil {
		t.Fatalf("read head: %v", err)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	again := make([]byte, 10)
	if _, err := io.ReadFull(r, again); err != nil {
		t.Fatalf("re-read head: %v", err)
	}
	if !bytes.Equal(head, again) {
		t.Fatalf("re-read gave %q, want %q", again, head)
	}
	if src.calls != 2 {
		t.Fatalf("%d requests, want 2 (a backward seek cannot reuse the stream)", src.calls)
	}
}

func TestRangeReaderSkipsForwardOnTheOpenStream(t *testing.T) {
	src := &fetchCounter{data: body(4 << 20)}
	r, err := NewRangeReader(context.Background(), src.fetch)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	if _, err := r.Read(make([]byte, 1)); err != nil {
		t.Fatalf("first read: %v", err)
	}
	// Well inside the skip window: must not cost a request.
	if _, err := r.Seek(64<<10, io.SeekStart); err != nil {
		t.Fatalf("short seek: %v", err)
	}
	one := make([]byte, 1)
	if _, err := io.ReadFull(r, one); err != nil {
		t.Fatalf("read after short seek: %v", err)
	}
	if one[0] != src.data[64<<10] {
		t.Fatalf("landed on %q, want %q", one, src.data[64<<10:(64<<10)+1])
	}
	if src.calls != 1 {
		t.Fatalf("%d requests, want 1 (a short hop forward is discarded from the stream)", src.calls)
	}

	// Far beyond it: a new request, at the right offset.
	if _, err := r.Seek(3<<20, io.SeekStart); err != nil {
		t.Fatalf("long seek: %v", err)
	}
	if _, err := io.ReadFull(r, one); err != nil {
		t.Fatalf("read after long seek: %v", err)
	}
	if one[0] != src.data[3<<20] {
		t.Fatalf("landed on %q, want %q", one, src.data[3<<20:(3<<20)+1])
	}
	if src.calls != 2 {
		t.Fatalf("%d requests, want 2", src.calls)
	}
	if src.offs[1] != 3<<20 {
		t.Fatalf("second request at %d, want %d", src.offs[1], 3<<20)
	}
}

func TestRangeReaderSeekFromTheEndBeforeReading(t *testing.T) {
	src := &fetchCounter{data: body(500)}
	r, err := NewRangeReader(context.Background(), src.fetch)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	// The size is only known once something has been fetched, so a seek from
	// the end has to discover it — this is the path an archive reader takes to
	// find a central directory.
	pos, err := r.Seek(-10, io.SeekEnd)
	if err != nil {
		t.Fatalf("seek end: %v", err)
	}
	if pos != 490 {
		t.Fatalf("position %d, want 490", pos)
	}
	tail, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read tail: %v", err)
	}
	if !bytes.Equal(tail, src.data[490:]) {
		t.Fatalf("tail = %q", tail)
	}
}

func TestRangeReaderStopsAtTheEnd(t *testing.T) {
	src := &fetchCounter{data: body(100)}
	r, err := NewRangeReader(context.Background(), src.fetch)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	if _, err := r.Seek(100, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if n, err := r.Read(make([]byte, 10)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("read at the end = (%d, %v), want (0, EOF)", n, err)
	}
	if _, err := r.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("a negative position must be refused")
	}
}

func TestRangeReaderReportsAMissingObjectOnOpen(t *testing.T) {
	failing := func(context.Context, int64) (io.ReadCloser, int64, error) {
		return nil, 0, errors.New("no such object")
	}
	// Callers use Open to learn whether the content exists at all, so the
	// failure must not be deferred to the first Read.
	if _, err := NewRangeReader(context.Background(), failing); err == nil {
		t.Fatal("a failed fetch must surface from the constructor")
	}
}
