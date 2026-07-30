package thumb

import (
	"archive/tar"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// TestSignJWT produces a verifiable HS256 token for the OnlyOffice convert API.
func TestSignJWT(t *testing.T) {
	secret := "s3cr3t"
	tok, err := signJWT(map[string]any{"filetype": "docx", "outputtype": "png"}, secret)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != want {
		t.Error("HS256 signature mismatch")
	}
	if hdr, _ := base64.RawURLEncoding.DecodeString(parts[0]); !strings.Contains(string(hdr), "HS256") {
		t.Errorf("header = %s, want HS256", hdr)
	}
}

func be32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func makeTar(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestEbookCBT: the cover of a TAR comic is its lowest-named image page.
func TestEbookCBT(t *testing.T) {
	cbt := makeTar(t, map[string][]byte{
		"02.png":    makePNG(t, 400, 600),
		"01.png":    makePNG(t, 120, 160), // cover
		"notes.txt": []byte("x"),
	})
	p := writeTemp(t, "comic.cbt", cbt)
	out, err := Ebook(p, ".cbt")
	if err != nil {
		t.Fatalf("Ebook cbt: %v", err)
	}
	assertJPEGThumb(t, out)
}

// makePSD builds a minimal PSD carrying a thumbnail (1036) image resource.
func makePSD(t *testing.T, preview []byte) []byte {
	t.Helper()
	var hdr bytes.Buffer
	hdr.WriteString("8BPS")
	hdr.Write([]byte{0, 1})    // version: PSD
	hdr.Write(make([]byte, 6)) // reserved
	hdr.Write([]byte{0, 3})    // channels
	hdr.Write(be32(600))       // height
	hdr.Write(be32(400))       // width
	hdr.Write([]byte{0, 8})    // depth
	hdr.Write([]byte{0, 3})    // color mode: RGB

	var res bytes.Buffer
	res.WriteString("8BIM")
	res.Write([]byte{0x04, 0x0C})                // resource id 1036 (thumbnail)
	res.Write([]byte{0, 0})                      // empty Pascal name, padded to even
	data := append(make([]byte, 28), preview...) // 28-byte thumb header + payload
	if len(data)%2 != 0 {
		data = append(data, 0)
	}
	res.Write(be32(uint32(len(data))))
	res.Write(data)

	var out bytes.Buffer
	out.Write(hdr.Bytes())
	out.Write(be32(0))                 // color-mode data length
	out.Write(be32(uint32(res.Len()))) // image-resources length
	out.Write(res.Bytes())
	return out.Bytes()
}

// TestPSDThumbnail extracts the embedded preview from a PSD.
func TestPSDThumbnail(t *testing.T) {
	p := writeTemp(t, "art.psd", makePSD(t, makePNG(t, 128, 192)))
	out, err := PSD(p)
	if err != nil {
		t.Fatalf("PSD: %v", err)
	}
	assertJPEGThumb(t, out)
}

// TestPSDNoThumbnail fails cleanly when the file carries no preview resource.
func TestPSDNoThumbnail(t *testing.T) {
	// Header + empty color-mode + empty image-resources sections.
	var b bytes.Buffer
	b.WriteString("8BPS")
	b.Write([]byte{0, 1})
	b.Write(make([]byte, 6))
	b.Write([]byte{0, 3})
	b.Write(be32(10))
	b.Write(be32(10))
	b.Write([]byte{0, 8})
	b.Write([]byte{0, 3})
	b.Write(be32(0)) // color-mode len
	b.Write(be32(0)) // image-resources len
	p := writeTemp(t, "empty.psd", b.Bytes())
	if _, err := PSD(p); err == nil {
		t.Error("expected error for PSD without a thumbnail resource")
	}
}

// TestFontSpecimen renders a specimen for a real TrueType font.
func TestFontSpecimen(t *testing.T) {
	out, err := FontSpecimen(goregular.TTF)
	if err != nil {
		t.Fatalf("FontSpecimen: %v", err)
	}
	assertJPEGThumb(t, out)
}

// makeICO builds a single-entry icon whose image is a PNG.
func makeICO(t *testing.T, png []byte) []byte {
	t.Helper()
	le := binary.LittleEndian
	var b bytes.Buffer
	hdr := make([]byte, 6)
	le.PutUint16(hdr[2:4], 1) // type: icon
	le.PutUint16(hdr[4:6], 1) // count
	b.Write(hdr)
	e := make([]byte, 16)
	e[0], e[1] = 64, 64 // width, height
	le.PutUint32(e[8:12], uint32(len(png)))
	le.PutUint32(e[12:16], 6+16) // offset after header + entry
	b.Write(e)
	b.Write(png)
	return b.Bytes()
}

// TestICO decodes the largest image out of a Windows icon.
func TestICO(t *testing.T) {
	p := writeTemp(t, "icon.ico", makeICO(t, makePNG(t, 64, 64)))
	out, err := ICO(p)
	if err != nil {
		t.Fatalf("ICO: %v", err)
	}
	assertJPEGThumb(t, out)
}

// TestExtraKinds maps the new extensions to the right strategies.
func TestExtraKinds(t *testing.T) {
	cases := map[string]string{
		".psd": KindPSD, ".psb": KindPSD,
		".ttf": KindFont, ".otf": KindFont, ".ttc": KindFont,
		".cbt": KindEbook,
		".eps": KindDocument, ".ai": KindDocument,
		".svg": KindSVG, ".svgz": KindSVG,
		".ico": KindICO, ".tga": KindVIPS,
	}
	for ext, want := range cases {
		if got := Kind(ext); got != want {
			t.Errorf("Kind(%q) = %q, want %q", ext, got, want)
		}
	}
	for _, k := range []string{KindPSD, KindFont, KindICO} {
		if !Available(k) {
			t.Errorf("%s should be available (pure Go)", k)
		}
	}
}
