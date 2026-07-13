package encrypt

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

type memRSC struct{ *bytes.Reader }

func (memRSC) Close() error { return nil }

func newCipherT(t *testing.T) *Cipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	c, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// ciphertext encrypts plaintext fully and returns the bytes.
func ciphertext(t *testing.T, c *Cipher, plaintext []byte) ([]byte, []byte) {
	t.Helper()
	er, iv, err := c.EncryptReader(bytes.NewReader(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	ct, err := io.ReadAll(er)
	if err != nil {
		t.Fatal(err)
	}
	if len(ct) != len(plaintext) {
		t.Fatalf("ciphertext length %d != plaintext %d", len(ct), len(plaintext))
	}
	return ct, iv
}

func TestRoundTrip(t *testing.T) {
	c := newCipherT(t)
	plaintext := make([]byte, 5000)
	_, _ = rand.Read(plaintext)
	ct, iv := ciphertext(t, c, plaintext)

	dec, err := c.DecryptReadSeeker(memRSC{bytes.NewReader(ct)}, iv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatal("decrypted content differs from plaintext")
	}
}

func TestSeek(t *testing.T) {
	c := newCipherT(t)
	plaintext := make([]byte, 5000)
	_, _ = rand.Read(plaintext)
	ct, iv := ciphertext(t, c, plaintext)

	dec, err := c.DecryptReadSeeker(memRSC{bytes.NewReader(ct)}, iv)
	if err != nil {
		t.Fatal(err)
	}

	// Seek to offsets that are and are not block-aligned.
	for _, off := range []int64{0, 1, 15, 16, 17, 100, 1234, 4096, 4999} {
		if _, err := dec.Seek(off, io.SeekStart); err != nil {
			t.Fatalf("seek %d: %v", off, err)
		}
		buf := make([]byte, 33)
		n, _ := io.ReadFull(dec, buf)
		want := plaintext[off:]
		if int64(len(want)) > int64(n) {
			want = want[:n]
		}
		if !bytes.Equal(buf[:n], want) {
			t.Fatalf("seek %d: decrypted %x, want %x", off, buf[:n], want)
		}
	}

	// Seek from end.
	if _, err := dec.Seek(-10, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	tail, _ := io.ReadAll(dec)
	if !bytes.Equal(tail, plaintext[len(plaintext)-10:]) {
		t.Fatalf("seek end: got %x", tail)
	}
}

func TestKeyLength(t *testing.T) {
	if _, err := NewCipher(make([]byte, 16)); err == nil {
		t.Fatal("expected error for non-32-byte key")
	}
}
