// Package encrypt provides AES-256-CTR encryption of stored objects at rest.
// CTR is a stream cipher: ciphertext and plaintext have the same length (no
// padding), and any byte offset can be decrypted independently, so decrypted
// reads stay seekable — which HTTP range/download serving relies on.
package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// BlockSize is the AES block size in bytes; also the IV length.
const BlockSize = aes.BlockSize

// Cipher encrypts and decrypts object streams with one AES-256 key.
type Cipher struct{ block cipher.Block }

// NewCipher builds a Cipher from a 32-byte (AES-256) key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encrypt: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &Cipher{block: block}, nil
}

// NewCipherHex builds a Cipher from a 64-char hex-encoded 32-byte key.
func NewCipherHex(s string) (*Cipher, error) {
	key, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("encrypt: invalid hex key: %w", err)
	}
	return NewCipher(key)
}

// EncryptReader wraps a plaintext reader so it yields ciphertext, returning the
// random IV that DecryptReadSeeker needs to reverse it.
func (c *Cipher) EncryptReader(r io.Reader) (io.Reader, []byte, error) {
	iv := make([]byte, BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, nil, err
	}
	stream := cipher.NewCTR(c.block, iv)
	return cipher.StreamReader{S: stream, R: r}, iv, nil
}

// ReadSeekCloser is a seekable, closable reader (matches the storage driver's).
type ReadSeekCloser interface {
	io.ReadSeeker
	io.Closer
}

// DecryptReadSeeker wraps a ciphertext reader so reads return plaintext, keeping
// it seekable by re-deriving the keystream at each seek target.
func (c *Cipher) DecryptReadSeeker(src ReadSeekCloser, iv []byte) (ReadSeekCloser, error) {
	if len(iv) != BlockSize {
		return nil, errors.New("encrypt: iv must be one block")
	}
	d := &decReader{src: src, block: c.block, iv: iv}
	d.resetStream(0)
	return d, nil
}

// decReader decrypts a CTR ciphertext stream with random-access support.
type decReader struct {
	src    ReadSeekCloser
	block  cipher.Block
	iv     []byte
	stream cipher.Stream
	off    int64
}

func (d *decReader) Read(p []byte) (int, error) {
	n, err := d.src.Read(p)
	if n > 0 {
		d.stream.XORKeyStream(p[:n], p[:n])
		d.off += int64(n)
	}
	return n, err
}

func (d *decReader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = d.off + offset
	case io.SeekEnd:
		end, err := d.src.Seek(0, io.SeekEnd)
		if err != nil {
			return 0, err
		}
		abs = end + offset
	default:
		return 0, errors.New("encrypt: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("encrypt: negative position")
	}
	if _, err := d.src.Seek(abs, io.SeekStart); err != nil {
		return 0, err
	}
	d.resetStream(abs)
	d.off = abs
	return abs, nil
}

func (d *decReader) Close() error { return d.src.Close() }

// resetStream positions the keystream so the next byte decrypts plaintext[abs].
func (d *decReader) resetStream(abs int64) {
	blockIndex := abs / BlockSize
	intra := abs % BlockSize
	d.stream = cipher.NewCTR(d.block, addCounter(d.iv, blockIndex))
	if intra > 0 {
		skip := make([]byte, intra)
		d.stream.XORKeyStream(skip, skip)
	}
}

// addCounter returns iv (a big-endian 128-bit counter) incremented by n.
func addCounter(iv []byte, n int64) []byte {
	c := make([]byte, len(iv))
	copy(c, iv)
	sum := uint64(n)
	for i := len(c) - 1; i >= 0; i-- {
		sum += uint64(c[i])
		c[i] = byte(sum)
		sum >>= 8
	}
	return c
}
