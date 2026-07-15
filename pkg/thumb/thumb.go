// Package thumb generates small JPEG preview thumbnails. Basic images are handled
// in pure Go; richer sources use external tools when present on the host:
// ffmpeg (video frames, audio cover art), libvips (extended image formats such as
// HEIC/AVIF/TIFF), LibRaw (camera RAW), and LibreOffice + poppler (Office/ODF
// documents and PDF). Each external generator is optional and skipped when its
// binary is missing.
package thumb

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
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

// Thumbnail strategies returned by Kind.
const (
	KindImage    = "image"    // built-in Go (jpg/png/gif)
	KindVIPS     = "vips"     // libvips (webp/heic/avif/tiff/...)
	KindRaw      = "raw"      // LibRaw (camera RAW)
	KindVideo    = "video"    // ffmpeg
	KindAudio    = "audio"    // ffmpeg cover art
	KindDocument = "document" // LibreOffice -> PDF -> poppler
	KindPDF      = "pdf"      // poppler
)

// Kind reports the thumbnail strategy for a file extension, or "" if none.
func Kind(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "jpg", "jpeg", "png", "gif":
		return KindImage
	case "webp", "tiff", "tif", "bmp", "heic", "heif", "avif", "jxl", "jp2", "jpx":
		return KindVIPS
	case "cr2", "cr3", "nef", "nrw", "arw", "sr2", "srf", "dng", "raf", "orf",
		"rw2", "pef", "srw", "k25", "kdc", "dcr", "mrw", "x3f", "3fr", "mef", "iiq", "mos", "raw":
		return KindRaw
	case "mp4", "mov", "webm", "mkv", "m4v", "avi":
		return KindVideo
	case "mp3", "flac", "m4a", "aac", "ogg", "opus":
		return KindAudio
	case "docx", "doc", "odt", "rtf", "xlsx", "xls", "ods", "pptx", "ppt", "odp":
		return KindDocument
	case "pdf":
		return KindPDF
	default:
		return ""
	}
}

// has reports whether an external binary is on PATH.
func has(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// FFmpegAvailable reports whether ffmpeg is present (video/audio thumbnails).
func FFmpegAvailable() bool { return has("ffmpeg") }

// VipsAvailable reports whether libvips is present (extended image formats).
func VipsAvailable() bool { return has("vips") }

// RawAvailable reports whether a LibRaw tool is present (camera RAW).
func RawAvailable() bool { return has("simple_dcraw") || has("dcraw_emu") }

// PopplerAvailable reports whether pdftoppm is present (PDF rasterisation).
func PopplerAvailable() bool { return has("pdftoppm") }

// LibreOfficeAvailable reports whether documents can be thumbnailed (LibreOffice
// converts them to PDF, which poppler then rasterises).
func LibreOfficeAvailable() bool {
	return (has("soffice") || has("libreoffice")) && PopplerAvailable()
}

// Available reports whether the host can generate a thumbnail for the strategy.
func Available(kind string) bool {
	switch kind {
	case KindImage:
		return true
	case KindVideo, KindAudio:
		return FFmpegAvailable()
	case KindVIPS:
		return VipsAvailable()
	case KindRaw:
		return RawAvailable()
	case KindPDF:
		return PopplerAvailable()
	case KindDocument:
		return LibreOfficeAvailable()
	default:
		return false
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

// Audio extracts embedded cover art from the audio file at path and returns it
// as a JPEG thumbnail. Fails when the file has no embedded artwork.
func Audio(ctx context.Context, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-loglevel", "error",
		"-i", path,
		"-an", "-map", "0:v:0",
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", MaxDim),
		"-f", "image2", "-vcodec", "mjpeg",
		"-",
	)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil || out.Len() == 0 {
		return nil, fmt.Errorf("thumb: no cover art: %s", strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), nil
}

// VIPS thumbnails an extended-format image (HEIC/AVIF/TIFF/WebP/...) with libvips,
// which auto-orients and scales within MaxDim.
func VIPS(ctx context.Context, path string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "thumb-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "out.jpg")
	cmd := exec.CommandContext(ctx, "vips", "thumbnail", path,
		fmt.Sprintf("%s[Q=%d]", out, jpegQuality), fmt.Sprintf("%d", MaxDim))
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("thumb: vips: %v: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return os.ReadFile(out)
}

// Raw extracts the embedded preview from a camera RAW file with LibRaw and scales
// it down.
func Raw(ctx context.Context, path string) ([]byte, error) {
	bin := "simple_dcraw"
	if !has(bin) {
		bin = "dcraw_emu"
	}
	cmd := exec.CommandContext(ctx, bin, "-e", path)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("thumb: libraw: %v: %s", err, strings.TrimSpace(errBuf.String()))
	}
	// LibRaw's -e writes the embedded preview next to the input; its exact name
	// varies (with/without the source extension, jpg/tiff/ppm), so try each.
	dir, base := filepath.Dir(path), filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	for _, cand := range []string{base, stem} {
		for _, ext := range []string{".thumb.jpg", ".thumb.jpeg", ".thumb.tiff", ".thumb.ppm"} {
			out := filepath.Join(dir, cand+ext)
			data, err := os.ReadFile(out)
			if err != nil {
				continue
			}
			os.Remove(out)
			// Scale the (often full-size) preview down; if it is a format the
			// built-in decoder can't read, return it as-is.
			if t, err := Image(data); err == nil {
				return t, nil
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("thumb: libraw produced no preview")
}

// PDF rasterises the first page of a PDF to a JPEG thumbnail with poppler.
func PDF(ctx context.Context, path string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "thumb-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	prefix := filepath.Join(dir, "page")
	cmd := exec.CommandContext(ctx, "pdftoppm", "-jpeg", "-f", "1", "-l", "1",
		"-scale-to", fmt.Sprintf("%d", MaxDim), path, prefix)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("thumb: pdftoppm: %v: %s", err, strings.TrimSpace(errBuf.String()))
	}
	matches, _ := filepath.Glob(prefix + "-*.jpg")
	if len(matches) == 0 {
		return nil, fmt.Errorf("thumb: pdftoppm produced no page")
	}
	return os.ReadFile(matches[0])
}

// Document renders the first page of an Office/ODF document by converting it to
// PDF with LibreOffice, then rasterising that PDF with poppler.
func Document(ctx context.Context, path string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "thumb-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	bin := "soffice"
	if !has(bin) {
		bin = "libreoffice"
	}
	cmd := exec.CommandContext(ctx, bin, "--headless", "--convert-to", "pdf", "--outdir", dir, path)
	// LibreOffice needs a writable profile; isolate it in the temp dir.
	cmd.Env = append(os.Environ(), "HOME="+dir)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("thumb: libreoffice: %v: %s", err, strings.TrimSpace(errBuf.String()))
	}
	pdfs, _ := filepath.Glob(filepath.Join(dir, "*.pdf"))
	if len(pdfs) == 0 {
		return nil, fmt.Errorf("thumb: libreoffice produced no pdf")
	}
	return PDF(ctx, pdfs[0])
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
