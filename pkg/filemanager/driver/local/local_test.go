package local

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
)

func newDriver(t *testing.T) driver.Handler {
	t.Helper()
	h, err := driver.New(&model.StoragePolicy{Type: model.PolicyTypeLocal, BasePath: t.TempDir()})
	if err != nil {
		t.Fatalf("new driver: %v", err)
	}
	return h
}

func TestPutOpenDelete(t *testing.T) {
	h := newDriver(t)
	ctx := context.Background()
	want := []byte("hello orion")

	if err := h.Put(ctx, "u1/file.bin", bytes.NewReader(want), int64(len(want))); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := h.Open(ctx, "u1/file.bin")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, want) {
		t.Fatalf("read = %q, want %q", got, want)
	}

	if _, err := h.Delete(ctx, "u1/file.bin"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := h.Open(ctx, "u1/file.bin"); err == nil {
		t.Fatal("expected open to fail after delete")
	}
}

func TestPathTraversalContained(t *testing.T) {
	base := t.TempDir()
	h, err := driver.New(&model.StoragePolicy{Type: model.PolicyTypeLocal, BasePath: base})
	if err != nil {
		t.Fatalf("new driver: %v", err)
	}
	ctx := context.Background()
	// A traversal attempt must never write outside base. Our resolver roots the
	// path inside base, so the write stays contained and round-trips.
	if err := h.Put(ctx, "../../escape.bin", bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(base), "escape.bin")); err == nil {
		t.Fatal("traversal escaped the base directory")
	}
}
