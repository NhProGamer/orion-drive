package thumb

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"testing"
)

// makeZip builds an in-memory ZIP from name->content entries.
func makeZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestEbookCBZ: the cover of a comic archive is the first image page by name.
func TestEbookCBZ(t *testing.T) {
	pngA := makePNG(t, 120, 160) // "01.png" — should win
	pngB := makePNG(t, 500, 700) // "02.png"
	cbz := makeZip(t, map[string][]byte{"02.png": pngB, "01.png": pngA, "meta.txt": []byte("x")})
	p := writeTemp(t, "comic.cbz", cbz)

	out, err := Ebook(p, ".cbz")
	if err != nil {
		t.Fatalf("Ebook cbz: %v", err)
	}
	assertJPEGThumb(t, out)
}

// TestEbookEPUB: the cover declared in the OPF manifest is extracted.
func TestEbookEPUB(t *testing.T) {
	cover := makePNG(t, 300, 450)
	container := []byte(`<?xml version="1.0"?>
<container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`)
	opf := []byte(`<?xml version="1.0"?>
<package><metadata><meta name="cover" content="cover-img"/></metadata>
<manifest><item id="cover-img" href="images/cover.png" media-type="image/png"/></manifest></package>`)
	epub := makeZip(t, map[string][]byte{
		"META-INF/container.xml": container,
		"OEBPS/content.opf":      opf,
		"OEBPS/images/cover.png": cover,
		"OEBPS/images/other.png": makePNG(t, 10, 10),
	})
	p := writeTemp(t, "book.epub", epub)

	out, err := Ebook(p, ".epub")
	if err != nil {
		t.Fatalf("Ebook epub: %v", err)
	}
	assertJPEGThumb(t, out)
}

// TestEbookFB2: the base64 cover binary referenced by <coverpage> is decoded.
func TestEbookFB2(t *testing.T) {
	png := makePNG(t, 200, 260)
	b64 := base64.StdEncoding.EncodeToString(png)
	fb2 := []byte(`<?xml version="1.0"?>
<FictionBook><description><title-info><coverpage><image href="#cov"/></coverpage></title-info></description>
<binary id="cov" content-type="image/png">` + b64 + `</binary></FictionBook>`)
	p := writeTemp(t, "book.fb2", fb2)

	out, err := Ebook(p, ".fb2")
	if err != nil {
		t.Fatalf("Ebook fb2: %v", err)
	}
	assertJPEGThumb(t, out)
}

// TestEbookKind wires the new extensions to the ebook strategy.
func TestEbookKind(t *testing.T) {
	for _, ext := range []string{".epub", ".cbz", ".cbt", ".fb2", ".mobi", ".azw", ".azw3"} {
		if k := Kind(ext); k != KindEbook {
			t.Errorf("Kind(%q) = %q, want %q", ext, k, KindEbook)
		}
	}
	if !Available(KindEbook) {
		t.Error("KindEbook should be available (pure Go)")
	}
}
