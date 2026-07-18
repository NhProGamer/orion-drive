package filemanager

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestBoundedReader(t *testing.T) {
	// Reading within the cap is unaffected.
	b, err := io.ReadAll(&boundedReader{r: strings.NewReader("hello"), max: 10})
	if err != nil || string(b) != "hello" {
		t.Fatalf("within cap: %q %v", b, err)
	}

	// A stream that runs past its cap (a lying header / zip bomb) is aborted.
	if _, err := io.ReadAll(&boundedReader{r: strings.NewReader("hello world"), max: 4}); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("expected ErrArchiveTooLarge, got %v", err)
	}

	// Exactly the cap is allowed (EOF lands on the boundary).
	b, err = io.ReadAll(&boundedReader{r: strings.NewReader("abcd"), max: 4})
	if err != nil || string(b) != "abcd" {
		t.Fatalf("exact cap: %q %v", b, err)
	}
}
