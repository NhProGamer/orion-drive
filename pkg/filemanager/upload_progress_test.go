package filemanager

import (
	"testing"
	"time"

	"github.com/NhProGamer/orion-drive/pkg/cache"
)

// TestUploadProgress proves the status derives per-chunk received state and the
// complete flag from the per-chunk cache markers (the field-based tracking it
// replaced was permanently zeroed).
func TestUploadProgress(t *testing.T) {
	m := &Manager{cache: cache.NewMemory()}
	id := "sess1"

	mask, complete := m.UploadProgress(id, 3)
	if complete || mask[0] || mask[1] || mask[2] {
		t.Fatalf("fresh session: complete=%v mask=%v", complete, mask)
	}

	_ = m.cache.Set(chunkKey(id, 0), []byte{1}, time.Minute)
	_ = m.cache.Set(chunkKey(id, 2), []byte{1}, time.Minute)
	mask, complete = m.UploadProgress(id, 3)
	if complete || !mask[0] || mask[1] || !mask[2] {
		t.Fatalf("partial: complete=%v mask=%v", complete, mask)
	}

	_ = m.cache.Set(chunkKey(id, 1), []byte{1}, time.Minute)
	if _, complete = m.UploadProgress(id, 3); !complete {
		t.Fatalf("all chunks present should be complete")
	}
}
