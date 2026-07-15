package thumb

import (
	"bytes"
	"context"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// minimalPDF is a valid one-page PDF (blank page) for the poppler test.
var minimalPDF = []byte("%PDF-1.4\n" +
	"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
	"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
	"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]>>endobj\n" +
	"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000052 00000 n \n0000000101 00000 n \n" +
	"trailer<</Size 4/Root 1 0 R>>\nstartxref\n170\n%%EOF")

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func assertJPEGThumb(t *testing.T, data []byte) {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("format = %s, want jpeg", format)
	}
	if b := img.Bounds(); b.Dx() > MaxDim || b.Dy() > MaxDim {
		t.Fatalf("thumbnail %dx%d exceeds MaxDim %d", b.Dx(), b.Dy(), MaxDim)
	}
}

func TestVIPSIntegration(t *testing.T) {
	if !VipsAvailable() {
		t.Skip("vips not installed")
	}
	png := writeTemp(t, "src.png", makePNG(t, 800, 600))
	webp := filepath.Join(filepath.Dir(png), "src.webp")
	if err := exec.Command("vips", "copy", png, webp).Run(); err != nil {
		t.Skipf("cannot synthesize webp input: %v", err)
	}
	out, err := VIPS(context.Background(), webp)
	if err != nil {
		t.Fatal(err)
	}
	assertJPEGThumb(t, out)
}

func TestPDFIntegration(t *testing.T) {
	if !PopplerAvailable() {
		t.Skip("poppler not installed")
	}
	out, err := PDF(context.Background(), writeTemp(t, "doc.pdf", minimalPDF))
	if err != nil {
		t.Fatal(err)
	}
	assertJPEGThumb(t, out)
}

func TestDocumentIntegration(t *testing.T) {
	if !LibreOfficeAvailable() {
		t.Skip("libreoffice or poppler not installed")
	}
	// RTF is a trivially-synthesizable document LibreOffice can convert to PDF.
	rtf := []byte(`{\rtf1\ansi\deff0 {\fonttbl{\f0 Arial;}}\f0\fs40 Hello OrionDrive thumbnail.\par}`)
	out, err := Document(context.Background(), writeTemp(t, "note.rtf", rtf))
	if err != nil {
		t.Fatal(err)
	}
	assertJPEGThumb(t, out)
}
