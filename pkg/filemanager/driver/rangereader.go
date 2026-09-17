package driver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
)

// RangeFetcher opens an object's bytes from off to its end, and reports the
// object's total size. Backends implement it with whatever ranged read they
// offer (an S3 GetObject with a Range, an HTTP request with a Range header).
//
// Returning the size here rather than taking it upfront means one request
// instead of two: every ranged response already carries the total, so no
// backend needs a separate HEAD.
type RangeFetcher func(ctx context.Context, off int64) (body io.ReadCloser, total int64, err error)

// seekWindow is how far ahead a forward seek is satisfied by discarding bytes
// from the stream already open, rather than paying for a new request. Sized so
// skipping over one entry's body in an archive usually stays on the open
// stream.
const seekWindow = 1 << 20 // 1 MiB

// readBuffer smooths out the many small reads an archive reader performs while
// walking headers, so each one does not reach the network.
const readBuffer = 128 << 10 // 128 KiB

// rangeReader presents an object as a ReadSeekCloser while holding at most one
// open stream.
//
// The point is to be cheap in both access patterns callers actually have:
// reading a file start to finish costs a single request, and hopping around
// (an archive's central directory, then its entries) costs one request per
// jump instead of buffering the whole object — which is what the S3 and remote
// backends used to do, whatever the caller needed.
//
// Not safe for concurrent use: one reader belongs to one caller, like the
// os.File the local backend returns.
type rangeReader struct {
	ctx   context.Context
	fetch RangeFetcher

	size int64 // -1 until a fetch reports it
	pos  int64 // where the caller is
	body io.ReadCloser
	br   *bufio.Reader
	at   int64 // where the open stream is, when body != nil
}

// NewRangeReader returns a ReadSeekCloser over an object, reading it in ranges.
//
// The first range is opened right away, so a missing object is reported here
// rather than on the first Read — Open has always failed fast for callers that
// only want to know whether the content exists. It costs nothing extra: a
// caller that reads from the start consumes exactly this stream.
func NewRangeReader(ctx context.Context, fetch RangeFetcher) (ReadSeekCloser, error) {
	r := &rangeReader{ctx: ctx, fetch: fetch, size: -1}
	if err := r.ensure(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rangeReader) Read(p []byte) (int, error) {
	if r.pos >= r.size {
		return 0, io.EOF
	}
	if err := r.ensure(); err != nil {
		return 0, err
	}
	// Never read past the object's end: a backend may keep the connection open
	// or pad, and the caller must see a clean EOF at size.
	if int64(len(p)) > r.size-r.pos {
		p = p[:r.size-r.pos]
	}
	n, err := r.br.Read(p)
	r.pos += int64(n)
	r.at += int64(n)
	if err == io.EOF && n > 0 {
		err = nil // report the bytes now, EOF on the next call
	}
	return n, err
}

func (r *rangeReader) Seek(offset int64, whence int) (int64, error) {
	var target int64
	switch whence {
	case io.SeekStart:
		target = offset
	case io.SeekCurrent:
		target = r.pos + offset
	case io.SeekEnd:
		target = r.size + offset
	default:
		return 0, fmt.Errorf("range reader: invalid whence %d", whence)
	}
	if target < 0 {
		return 0, errors.New("range reader: negative position")
	}
	if target == r.pos {
		return r.pos, nil
	}
	// A short hop forward is cheaper to skip than to re-request.
	if r.body != nil && target > r.at && target-r.at <= seekWindow {
		if _, err := r.br.Discard(int(target - r.at)); err == nil {
			r.at = target
			r.pos = target
			return r.pos, nil
		}
		// Discard failed (short object): fall through and reopen.
	}
	if target != r.at {
		r.release()
	}
	r.pos = target
	return r.pos, nil
}

func (r *rangeReader) Close() error {
	r.release()
	return nil
}

// ensure makes sure a stream is open at the caller's position.
func (r *rangeReader) ensure() error {
	if r.body != nil && r.at == r.pos {
		return nil
	}
	r.release()
	body, total, err := r.fetch(r.ctx, r.pos)
	if err != nil {
		return err
	}
	r.size = total
	r.body = body
	r.br = bufio.NewReaderSize(body, readBuffer)
	r.at = r.pos
	return nil
}

func (r *rangeReader) release() {
	if r.body != nil {
		_ = r.body.Close()
		r.body = nil
		r.br = nil
	}
}
