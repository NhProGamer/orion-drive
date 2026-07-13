package thumb

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageScalesDown(t *testing.T) {
	data := makePNG(t, 1000, 600)
	out, err := Image(data)
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("format = %s, want jpeg", format)
	}
	b := img.Bounds()
	if b.Dx() != MaxDim {
		t.Fatalf("width = %d, want %d", b.Dx(), MaxDim)
	}
	// Aspect ratio preserved: 1000x600 -> 400x240.
	if b.Dy() != 240 {
		t.Fatalf("height = %d, want 240", b.Dy())
	}
}

func TestImageKeepsSmall(t *testing.T) {
	data := makePNG(t, 120, 80)
	out, err := Image(data)
	if err != nil {
		t.Fatal(err)
	}
	img, _, _ := image.Decode(bytes.NewReader(out))
	if img.Bounds().Dx() != 120 {
		t.Fatalf("width = %d, want 120 (unchanged)", img.Bounds().Dx())
	}
}

func TestKind(t *testing.T) {
	cases := map[string]string{".jpg": "image", "png": "image", ".mp4": "video", ".txt": "", ".pdf": ""}
	for ext, want := range cases {
		if got := Kind(ext); got != want {
			t.Errorf("Kind(%q) = %q, want %q", ext, got, want)
		}
	}
}
