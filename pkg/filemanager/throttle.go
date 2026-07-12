package filemanager

import (
	"context"

	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"golang.org/x/time/rate"
)

// throttleBurst is the token-bucket burst size. It must exceed the largest
// single Read (http.ServeContent copies in 32 KiB chunks).
const throttleBurst = 256 << 10

// throttledReader wraps a ReadSeekCloser and caps its read throughput to a
// fixed number of bytes per second, so per-group download speed limits apply
// while OrionDrive streams a file. Seek and Close pass straight through.
type throttledReader struct {
	rsc     driver.ReadSeekCloser
	limiter *rate.Limiter
	ctx     context.Context
}

// newThrottledReader caps rsc at bytesPerSec (<= 0 means no limit — rsc is
// returned unwrapped).
func newThrottledReader(rsc driver.ReadSeekCloser, bytesPerSec int64) driver.ReadSeekCloser {
	if bytesPerSec <= 0 {
		return rsc
	}
	return &throttledReader{
		rsc:     rsc,
		limiter: rate.NewLimiter(rate.Limit(bytesPerSec), throttleBurst),
		ctx:     context.Background(),
	}
}

func (t *throttledReader) Read(p []byte) (int, error) {
	// Never request more tokens than the burst allows in one wait.
	if len(p) > throttleBurst {
		p = p[:throttleBurst]
	}
	n, err := t.rsc.Read(p)
	if n > 0 {
		if werr := t.limiter.WaitN(t.ctx, n); werr != nil {
			return n, werr
		}
	}
	return n, err
}

func (t *throttledReader) Seek(offset int64, whence int) (int64, error) {
	return t.rsc.Seek(offset, whence)
}

func (t *throttledReader) Close() error {
	return t.rsc.Close()
}
