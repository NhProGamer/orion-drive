// Package thumb generates small JPEG preview thumbnails. Images are handled in
// pure Go; videos use ffmpeg when it is available on the host.
package thumb

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os/exec"
	"strings"

	// Register the image decoders used by the built-in generator.
	_ "image/gif"
	_ "image/png"

	"golang.org/x/image/draw"
)

// MaxDim is the longest edge (px) of a generated thumbnail.
const MaxDim = 400

// jpegQuality is the encoding quality for thumbnails.
const jpegQuality = 82

// Kind reports the thumbnail strategy for a file extension, or "" if none.
func Kind(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "jpg", "jpeg", "png", "gif":
		return "image"
	case "mp4", "mov", "webm", "mkv", "m4v", "avi":
		return "video"
	default:
		return ""
	}
}

// Image decodes an image and returns a scaled-down JPEG thumbnail.
func Image(data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("thumb: decode: %w", err)
	}
	return encode(scale(src)), nil
}

// FFmpegAvailable reports whether the ffmpeg binary can be found.
func FFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// Video extracts a frame from the video at path and returns a JPEG thumbnail.
func Video(ctx context.Context, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-loglevel", "error",
		"-ss", "1",
		"-i", path,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", MaxDim),
		"-f", "image2", "-vcodec", "mjpeg",
		"-",
	)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("thumb: ffmpeg: %v: %s", err, strings.TrimSpace(errBuf.String()))
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("thumb: ffmpeg produced no frame")
	}
	return out.Bytes(), nil
}

// scale resizes src so its longest edge is at most MaxDim, preserving aspect.
func scale(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= MaxDim && h <= MaxDim {
		return src
	}
	tw, th := w, h
	if w >= h {
		tw = MaxDim
		th = h * MaxDim / w
	} else {
		th = MaxDim
		tw = w * MaxDim / h
	}
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})
	return buf.Bytes()
}
