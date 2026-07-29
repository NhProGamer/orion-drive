package thumb

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// fontSpecimenLines are drawn as a preview of a font file.
var fontSpecimenLines = []string{"Aa Bb Cc", "0123456789"}

// FontSpecimen renders a small specimen image (a few sample glyphs) for a
// TrueType/OpenType font, so font files get a preview instead of a generic
// icon. TTC collections use their first face. Returns an error for fonts that
// cannot be parsed or expose no drawable Latin glyphs.
func FontSpecimen(data []byte) ([]byte, error) {
	f, err := parseFont(data)
	if err != nil {
		return nil, err
	}
	const size = 96
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer face.Close()

	m := face.Metrics()
	lineH := (m.Ascent + m.Descent).Ceil() + size/4
	width := 0
	for _, s := range fontSpecimenLines {
		if w := font.MeasureString(face, s).Ceil(); w > width {
			width = w
		}
	}
	if width <= 0 {
		return nil, fmt.Errorf("thumb: font has no drawable specimen glyphs")
	}
	pad := size / 2
	W := width + pad*2
	H := lineH*len(fontSpecimenLines) + pad*2

	img := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.RGBA{0x18, 0x18, 0x18, 0xff}), Face: face}
	y := pad + m.Ascent.Ceil()
	for _, s := range fontSpecimenLines {
		d.Dot = fixed.P(pad, y)
		d.DrawString(s)
		y += lineH
	}
	return encode(scale(img)), nil
}

// parseFont parses a single font, falling back to the first face of a
// TrueType/OpenType collection (TTC/OTC).
func parseFont(data []byte) (*sfnt.Font, error) {
	if f, err := opentype.Parse(data); err == nil {
		return f, nil
	}
	coll, err := opentype.ParseCollection(data)
	if err != nil {
		return nil, err
	}
	return coll.Font(0)
}
