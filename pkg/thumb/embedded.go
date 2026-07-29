package thumb

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// PSD extracts the JPEG preview that Photoshop embeds in a PSD/PSB file's image
// resources (resource 1036, or the older BGR 1033) and scales it down. Only the
// header and the image-resources block are read, so multi-gigabyte documents are
// cheap. It fails when the file carries no embedded preview.
func PSD(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReader(f)

	// File header: "8BPS" signature + 22 bytes we don't need here.
	hdr := make([]byte, 26)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	if string(hdr[0:4]) != "8BPS" {
		return nil, fmt.Errorf("thumb: not a PSD")
	}
	// Color-mode data section: length-prefixed, skipped.
	if err := skipSection(r); err != nil {
		return nil, err
	}
	// Image-resources section: length-prefixed block of 8BIM resources.
	var irLen uint32
	if err := binary.Read(r, binary.BigEndian, &irLen); err != nil {
		return nil, err
	}
	if irLen == 0 || irLen > 64<<20 {
		return nil, fmt.Errorf("thumb: no PSD image resources")
	}
	res := make([]byte, irLen)
	if _, err := io.ReadFull(r, res); err != nil {
		return nil, err
	}
	jpeg, err := psdThumbFromResources(res)
	if err != nil {
		return nil, err
	}
	return Image(jpeg)
}

// skipSection consumes a 4-byte-length-prefixed section.
func skipSection(r *bufio.Reader) error {
	var n uint32
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return err
	}
	_, err := io.CopyN(io.Discard, r, int64(n))
	return err
}

// psdThumbFromResources scans the image-resources block for a thumbnail resource
// (1036 preferred, 1033 fallback) and returns its embedded JPEG bytes.
func psdThumbFromResources(res []byte) ([]byte, error) {
	be := binary.BigEndian
	for pos := 0; pos+12 <= len(res); {
		if string(res[pos:pos+4]) != "8BIM" {
			break
		}
		id := be.Uint16(res[pos+4 : pos+6])
		// Pascal name: length byte + name, padded so the pair is even.
		nameLen := int(res[pos+6])
		nameField := nameLen + 1
		if nameField%2 != 0 {
			nameField++
		}
		q := pos + 6 + nameField
		if q+4 > len(res) {
			break
		}
		size := int(be.Uint32(res[q : q+4]))
		data := q + 4
		if data+size > len(res) {
			break
		}
		// Thumbnail resource layout: 28-byte header then the JPEG payload.
		if (id == 1036 || id == 1033) && size > 28 {
			return res[data+28 : data+size], nil
		}
		// Advance to the next resource; data is padded to an even length.
		adv := size
		if adv%2 != 0 {
			adv++
		}
		pos = data + adv
	}
	return nil, fmt.Errorf("thumb: no embedded PSD thumbnail")
}
