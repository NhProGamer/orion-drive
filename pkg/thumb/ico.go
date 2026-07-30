package thumb

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// ICO renders a Windows icon (.ico) thumbnail in pure Go, picking the
// largest embedded image. Icon entries hold either a PNG (returned as-is) or a
// headerless DIB, which is wrapped back into a BMP for the built-in decoder.
func ICO(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCoverSource))
	if err != nil {
		return nil, err
	}
	img, err := icoBestImage(data)
	if err != nil {
		return nil, err
	}
	return Image(img)
}

// icoBestImage returns the decodable bytes of the highest-resolution entry.
func icoBestImage(data []byte) ([]byte, error) {
	le := binary.LittleEndian
	if len(data) < 6 || le.Uint16(data[0:2]) != 0 {
		return nil, fmt.Errorf("thumb: not an icon")
	}
	count := int(le.Uint16(data[4:6]))
	if count == 0 {
		return nil, fmt.Errorf("thumb: empty icon")
	}
	bestArea := -1
	var bestOff, bestSize int
	for i := 0; i < count; i++ {
		e := 6 + i*16
		if e+16 > len(data) {
			break
		}
		w, h := int(data[e]), int(data[e+1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		size := int(le.Uint32(data[e+8 : e+12]))
		off := int(le.Uint32(data[e+12 : e+16]))
		if size <= 0 || off <= 0 || off+size > len(data) {
			continue
		}
		if area := w * h; area > bestArea {
			bestArea, bestOff, bestSize = area, off, size
		}
	}
	if bestArea < 0 {
		return nil, fmt.Errorf("thumb: no usable icon image")
	}
	blob := data[bestOff : bestOff+bestSize]
	if isImageMagic(blob) { // PNG/JPEG stored directly
		return blob, nil
	}
	return dibToBMP(blob)
}

// dibToBMP wraps a headerless DIB (as stored in an icon) into a BMP file the
// built-in decoder can read, halving the height to drop the AND mask.
func dibToBMP(dib []byte) ([]byte, error) {
	le := binary.LittleEndian
	if len(dib) < 40 {
		return nil, fmt.Errorf("thumb: short DIB")
	}
	biSize := le.Uint32(dib[0:4])
	if int(biSize) > len(dib) {
		return nil, fmt.Errorf("thumb: bad DIB header")
	}
	out := make([]byte, len(dib))
	copy(out, dib)
	// Icon DIBs report double height (XOR colour data + AND mask); use half.
	h := int32(le.Uint32(dib[8:12]))
	le.PutUint32(out[8:12], uint32(h/2))

	var palette uint32
	if bitCount := le.Uint16(dib[14:16]); bitCount <= 8 {
		colors := le.Uint32(dib[32:36])
		if colors == 0 {
			colors = 1 << bitCount
		}
		palette = colors * 4
	}
	offBits := 14 + biSize + palette

	hdr := make([]byte, 14)
	hdr[0], hdr[1] = 'B', 'M'
	le.PutUint32(hdr[2:6], uint32(14+len(out)))
	le.PutUint32(hdr[10:14], offBits)
	return append(hdr, out...), nil
}
