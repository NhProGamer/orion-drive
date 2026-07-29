// Package thumb generates small JPEG preview thumbnails. Basic images are handled
// in pure Go; richer sources use external tools when present on the host:
// ffmpeg (video frames, audio cover art), libvips (extended image formats such as
// HEIC/AVIF/TIFF), LibRaw (camera RAW), and LibreOffice + poppler (Office/ODF
// documents and PDF). Each external generator is optional and skipped when its
// binary is missing. E-book / comic covers (EPUB/CBZ/CBT/FB2/MOBI/AZW3),
// Photoshop previews (PSD/PSB) and font specimens (TTF/OTF/TTC) are handled in
// pure Go.
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
	"sync"

	// Register the image decoders used by the built-in generator. WebP, BMP and
	// TIFF are pure-Go, so those formats no longer need the vips binary.
	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"golang.org/x/image/draw"
)

// Defaults for the tunable settings; overridden by Configure.
const (
	defaultMaxDim  = 400
	defaultQuality = 82
)

// Tunable settings, set at startup by Configure. maxDim is the longest edge (px)
// of a generated thumbnail; jpegQuality is the JPEG encoding quality.
var (
	maxDim      = defaultMaxDim
	jpegQuality = defaultQuality
)

// Thumbnail strategies returned by Kind.
const (
	KindImage    = "image"    // built-in Go (jpg/png/gif)
	KindVIPS     = "vips"     // libvips (webp/heic/avif/tiff/...)
	KindRaw      = "raw"      // LibRaw (camera RAW)
	KindVideo    = "video"    // ffmpeg
	KindAudio    = "audio"    // ffmpeg cover art
	KindDocument = "document" // LibreOffice -> PDF -> poppler
	KindPDF      = "pdf"      // poppler
	KindEbook    = "ebook"    // built-in Go (epub/cbz/fb2/mobi/azw3 cover art)
	KindPSD      = "psd"      // built-in Go (embedded Photoshop preview)
	KindFont     = "font"     // built-in Go (font specimen render)
)

// Kind reports the thumbnail strategy for a file extension, or "" if none.
func Kind(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff", "tif":
		return KindImage
	case "heic", "heif", "avif", "jxl", "jp2", "jpx", "svg",
		"ico", "tga", "xcf", "dds", "qoi", "pcx", "hdr":
		return KindVIPS
	case "cr2", "cr3", "nef", "nrw", "arw", "sr2", "srf", "dng", "raf", "orf",
		"rw2", "pef", "srw", "k25", "kdc", "dcr", "mrw", "x3f", "3fr", "mef", "iiq", "mos", "raw":
		return KindRaw
	case "mp4", "mov", "webm", "mkv", "m4v", "avi",
		"flv", "wmv", "mpg", "mpeg", "3gp", "3g2", "ts", "m2ts", "mts", "ogv", "asf":
		return KindVideo
	case "mp3", "flac", "m4a", "aac", "ogg", "opus", "wma", "aiff", "wav":
		return KindAudio
	case "epub", "cbz", "cbt", "fb2", "mobi", "azw", "azw3":
		return KindEbook
	case "psd", "psb":
		return KindPSD
	case "ttf", "otf", "ttc", "otc":
		return KindFont
	case "docx", "doc", "odt", "rtf", "xlsx", "xls", "ods", "pptx", "ppt", "odp",
		"eps", "ai":
		return KindDocument
	case "pdf":
		return KindPDF
	default:
		return ""
	}
}

// External-tool settings, set at startup by Configure. Empty binary paths fall
// back to the default names (resolved on PATH); the disable flags force a
// generator off even when its binary is present.
var (
	disabled                                                      bool
	binFFmpeg                                                     = "ffmpeg"
	binVips                                                       = "vips"
	binPoppler                                                    = "pdftoppm"
	binLibre                                                      string // "" → soffice, then libreoffice
	binLibRaw                                                     string // "" → simple_dcraw, then dcraw_emu
	disVideo, disAudio, disVips, disRaw, disPDF, disDoc, disEbook bool
)

// Options configures the thumbnail generators. A zero value keeps the defaults.
type Options struct {
	Disable                                                                                        bool // master switch: disable all thumbnails
	MaxDim, Quality                                                                                int  // 0 keeps the default
	DisableVideo, DisableAudio, DisableVips, DisableRaw, DisablePDF, DisableDocument, DisableEbook bool
	// Binary path overrides (empty = default name resolved on PATH).
	FFmpegPath, VipsPath, PopplerPath, LibreOfficePath, LibRawPath string
}

// Configure applies runtime settings; called once at startup.
func Configure(o Options) {
	disabled = o.Disable
	if o.MaxDim > 0 {
		maxDim = o.MaxDim
	}
	if o.Quality > 0 {
		jpegQuality = o.Quality
	}
	disVideo, disAudio = o.DisableVideo, o.DisableAudio
	disVips, disRaw = o.DisableVips, o.DisableRaw
	disPDF, disDoc = o.DisablePDF, o.DisableDocument
	disEbook = o.DisableEbook
	if o.FFmpegPath != "" {
		binFFmpeg = o.FFmpegPath
	}
	if o.VipsPath != "" {
		binVips = o.VipsPath
	}
	if o.PopplerPath != "" {
		binPoppler = o.PopplerPath
	}
	binLibre = o.LibreOfficePath
	binLibRaw = o.LibRawPath
}

// hasCache memoises binary-presence probes. exec.LookPath scans every $PATH
// entry, and Available() (hence has()) is hit on hot, unauthenticated paths (the
// share preview / thumbnail endpoints). Installed binaries don't come and go
// during a process's lifetime, so the result is cached per binary name; the
// trade-off is that a newly-installed tool is only picked up after a restart.
var (
	hasCacheMu sync.RWMutex
	hasCache   = map[string]bool{}
)

// has reports whether an external binary is on PATH (or is an explicit path),
// caching the lookup.
func has(bin string) bool {
	if bin == "" {
		return false
	}
	hasCacheMu.RLock()
	v, ok := hasCache[bin]
	hasCacheMu.RUnlock()
	if ok {
		return v
	}
	_, err := exec.LookPath(bin)
	v = err == nil
	hasCacheMu.Lock()
	hasCache[bin] = v
	hasCacheMu.Unlock()
	return v
}

// libreBin resolves the LibreOffice binary, honouring the override.
func libreBin() string {
	if binLibre != "" {
		return binLibre
	}
	if has("soffice") {
		return "soffice"
	}
	if has("libreoffice") {
		return "libreoffice"
	}
	return ""
}

// libRawBin resolves the LibRaw preview extractor, honouring the override.
func libRawBin() string {
	if binLibRaw != "" {
		return binLibRaw
	}
	if has("simple_dcraw") {
		return "simple_dcraw"
	}
	if has("dcraw_emu") {
		return "dcraw_emu"
	}
	return ""
}

// FFmpegAvailable reports whether ffmpeg is present (video/audio thumbnails).
func FFmpegAvailable() bool { return has(binFFmpeg) }

// VipsAvailable reports whether libvips is present (extended image formats).
func VipsAvailable() bool { return has(binVips) }

// RawAvailable reports whether a LibRaw tool is present (camera RAW).
func RawAvailable() bool { return libRawBin() != "" }

// PopplerAvailable reports whether pdftoppm is present (PDF rasterisation).
func PopplerAvailable() bool { return has(binPoppler) }

// LibreOfficeAvailable reports whether documents can be thumbnailed (LibreOffice
// converts them to PDF, which poppler then rasterises).
func LibreOfficeAvailable() bool {
	return libreBin() != "" && PopplerAvailable()
}

// Available reports whether the host can generate a thumbnail for the strategy,
// honouring both binary presence and the configured disable flags.
func Available(kind string) bool {
	if disabled {
		return false
	}
	switch kind {
	case KindImage:
		return true
	case KindEbook:
		return !disEbook // pure Go, no external tool required
	case KindPSD, KindFont:
		return true // pure Go, no external tool required
	case KindVideo:
		return !disVideo && FFmpegAvailable()
	case KindAudio:
		return !disAudio && FFmpegAvailable()
	case KindVIPS:
		return !disVips && VipsAvailable()
	case KindRaw:
		return !disRaw && RawAvailable()
	case KindPDF:
		return !disPDF && PopplerAvailable()
	case KindDocument:
		return !disDoc && LibreOfficeAvailable()
	default:
		return false
	}
}

// maxImagePixels caps the decoded pixel count to defend against decompression
// bombs — a tiny file whose header declares a gigapixel image would otherwise
// allocate an enormous in-memory buffer. ~50 megapixels covers real photos.
const maxImagePixels = 50_000_000

// Image decodes an image and returns a scaled-down JPEG thumbnail.
func Image(data []byte) ([]byte, error) {
	// Check declared dimensions from the header (cheap, no full allocation)
	// before decoding the whole image.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("thumb: decode config: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, fmt.Errorf("thumb: image too large to thumbnail: %dx%d", cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("thumb: decode: %w", err)
	}
	return encode(scale(src)), nil
}

// Video extracts a frame from the video at path and returns a JPEG thumbnail.
func Video(ctx context.Context, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binFFmpeg,
		"-loglevel", "error",
		"-ss", "1",
		"-i", path,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", maxDim),
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
	cmd := exec.CommandContext(ctx, binFFmpeg,
		"-loglevel", "error",
		"-i", path,
		"-an", "-map", "0:v:0",
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", maxDim),
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
// which auto-orients and scales within maxDim.
func VIPS(ctx context.Context, path string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "thumb-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "out.jpg")
	cmd := exec.CommandContext(ctx, binVips, "thumbnail", path,
		fmt.Sprintf("%s[Q=%d]", out, jpegQuality), fmt.Sprintf("%d", maxDim))
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
	bin := libRawBin()
	if bin == "" {
		return nil, fmt.Errorf("thumb: no LibRaw tool available")
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
	cmd := exec.CommandContext(ctx, binPoppler, "-jpeg", "-f", "1", "-l", "1",
		"-scale-to", fmt.Sprintf("%d", maxDim), path, prefix)
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

	bin := libreBin()
	if bin == "" {
		return nil, fmt.Errorf("thumb: no LibreOffice binary available")
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

// scale resizes src so its longest edge is at most maxDim, preserving aspect.
func scale(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim {
		return src
	}
	tw, th := w, h
	if w >= h {
		tw = maxDim
		th = h * maxDim / w
	} else {
		th = maxDim
		tw = w * maxDim / h
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
