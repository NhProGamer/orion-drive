package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
	"github.com/ulikunitz/xz/lzma"
)

// Creatable lists the formats OrionDrive can produce, in the order a chooser
// should offer them: from the most widely readable to the most compact.
//
// 7-Zip is absent because no pure-Go writer for it exists — every Go library,
// this package's reader included, only reads 7z. Producing one would mean
// either cgo (this binary is built without it) or shelling out to the 7z tool,
// which cannot build an archive from object storage without first staging the
// whole tree on disk. tar.xz is the honest substitute: LZMA2 is the algorithm
// 7-Zip compresses with by default, and it streams.
//
// bzip2 stays read-only: the standard library has no writer for it.
var Creatable = []string{FormatZip, FormatTarGz, FormatTarZst, FormatTarXz}

// CanCreate reports whether a format can be produced.
func CanCreate(format string) bool {
	for _, f := range Creatable {
		if f == format {
			return true
		}
	}
	return false
}

// Extension returns the file extension a format's archives carry.
func Extension(format string) string {
	switch format {
	case FormatZip:
		return ".zip"
	case FormatTarGz:
		return ".tar.gz"
	case FormatTarZst:
		return ".tar.zst"
	case FormatTarXz:
		return ".tar.xz"
	default:
		return ""
	}
}

// EnsureExtension returns name with the format's extension, adding it unless
// the name already ends in one this format accepts.
func EnsureExtension(name, format string) string {
	lower := strings.ToLower(name)
	for _, s := range suffixes {
		if s.format == format && strings.HasSuffix(lower, s.ext) {
			return name
		}
	}
	return name + Extension(format)
}

// SplitExt splits a file name into its stem and extension, keeping multi-part
// archive extensions whole: "backup.tar.gz" splits as "backup" + ".tar.gz",
// not "backup.tar" + ".gz".
//
// It matters for anything that rebuilds a name around the extension. Naming a
// copy with path.Ext turns "backup.tar.gz" into "backup.tar (2).gz", which is
// no longer a name any archive reader recognises.
func SplitExt(name string) (stem, ext string) {
	lower := strings.ToLower(name)
	for _, s := range suffixes {
		if len(s.ext) > len(ext) && strings.HasSuffix(lower, s.ext) {
			ext = s.ext
		}
	}
	if ext == "" {
		return name, ""
	}
	// Return the extension as it was actually spelled, not lower-cased.
	cut := len(name) - len(ext)
	return name[:cut], name[cut:]
}

// Writer builds an archive one member at a time.
type Writer interface {
	// AddFile writes a file member of the given size, read from r.
	AddFile(name string, size int64, modified time.Time, r io.Reader) error
	// AddDir writes an explicit directory member, which is how an empty folder
	// survives the round trip.
	AddDir(name string) error
	// Close finishes the archive. It must be called for the result to be
	// readable: it is what writes a ZIP's central directory and flushes a
	// compressor's tail.
	Close() error
}

// NewWriter returns a Writer producing format into w.
//
// level is the compression effort, 1 (fastest) to 9 (smallest); 0 picks the
// format's default. It is clamped rather than rejected, since it comes from a
// user-facing setting.
func NewWriter(w io.Writer, format string, level int) (Writer, error) {
	switch format {
	case FormatZip:
		return newZipWriter(w, level), nil
	case FormatTarGz:
		return newTarGzWriter(w, level)
	case FormatTarZst:
		return newTarZstWriter(w, level)
	case FormatTarXz:
		return newTarXzWriter(w, level)
	default:
		return nil, fmt.Errorf("archive: cannot create %q archives", format)
	}
}

// --- ZIP --------------------------------------------------------------------

type zipWriter struct {
	zw *zip.Writer
}

func newZipWriter(w io.Writer, level int) *zipWriter {
	zw := zip.NewWriter(w)
	if level > 0 {
		// The default deflate level is the only one zip.Writer uses unless a
		// compressor is registered for the method.
		effort := clamp(level, flate.BestSpeed, flate.BestCompression)
		zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
			return flate.NewWriter(out, effort)
		})
	}
	return &zipWriter{zw: zw}
}

func (z *zipWriter) AddFile(name string, _ int64, modified time.Time, r io.Reader) error {
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified}
	// Without a mode, entries unpack with no permissions at all on some
	// extractors.
	hdr.SetMode(0o644)
	wr, err := z.zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(wr, r)
	return err
}

func (z *zipWriter) AddDir(name string) error {
	hdr := &zip.FileHeader{Name: strings.TrimSuffix(name, "/") + "/"}
	hdr.SetMode(os.ModeDir | 0o755)
	_, err := z.zw.CreateHeader(hdr)
	return err
}

func (z *zipWriter) Close() error { return z.zw.Close() }

// --- TAR variants -----------------------------------------------------------

// tarWriter writes a TAR through a compressor, closing both in order.
type tarWriter struct {
	tw         *tar.Writer
	compressor io.Closer
}

func (t *tarWriter) AddFile(name string, size int64, modified time.Time, r io.Reader) error {
	if err := t.tw.WriteHeader(&tar.Header{
		Name:     name,
		Size:     size,
		Mode:     0o644,
		ModTime:  modified,
		Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	// A TAR header commits to a length, so a source that turns out shorter or
	// longer than the size recorded for it corrupts the stream. CopyN with an
	// exact count fails loudly instead.
	if _, err := io.CopyN(t.tw, r, size); err != nil {
		return fmt.Errorf("archive %q: %w", name, err)
	}
	return nil
}

func (t *tarWriter) AddDir(name string) error {
	return t.tw.WriteHeader(&tar.Header{
		Name:     strings.TrimSuffix(name, "/") + "/",
		Mode:     0o755,
		Typeflag: tar.TypeDir,
	})
}

func (t *tarWriter) Close() error {
	if err := t.tw.Close(); err != nil {
		return err
	}
	if t.compressor != nil {
		return t.compressor.Close()
	}
	return nil
}

func newTarGzWriter(w io.Writer, level int) (Writer, error) {
	gzLevel := gzip.DefaultCompression
	if level > 0 {
		gzLevel = clamp(level, gzip.BestSpeed, gzip.BestCompression)
	}
	gw, err := gzip.NewWriterLevel(w, gzLevel)
	if err != nil {
		return nil, err
	}
	return &tarWriter{tw: tar.NewWriter(gw), compressor: gw}, nil
}

func newTarZstWriter(w io.Writer, level int) (Writer, error) {
	zw, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstdLevel(level)))
	if err != nil {
		return nil, err
	}
	return &tarWriter{tw: tar.NewWriter(zw), compressor: zw}, nil
}

func newTarXzWriter(w io.Writer, level int) (Writer, error) {
	cfg := xz.WriterConfig{DictCap: xzDictCap(level)}
	if level <= 3 && level > 0 {
		// The hash-table matcher trades ratio for speed; the binary tree is
		// what makes xz slow, and only worth it when compactness is the point.
		cfg.Matcher = lzma.HashTable4
	}
	xw, err := cfg.NewWriter(w)
	if err != nil {
		return nil, err
	}
	return &tarWriter{tw: tar.NewWriter(xw), compressor: xw}, nil
}

// xzDictCap maps the 1-9 scale onto a dictionary size, which is what decides
// both how well xz compresses and how much memory it needs.
func xzDictCap(level int) int {
	switch {
	case level <= 0:
		return 8 << 20 // the library's own default
	case level <= 3:
		return 1 << 20
	case level <= 6:
		return 8 << 20
	default:
		return 32 << 20
	}
}

// zstdLevel maps the 1-9 scale onto zstd's four levels.
func zstdLevel(level int) zstd.EncoderLevel {
	switch {
	case level <= 0:
		return zstd.SpeedDefault
	case level <= 2:
		return zstd.SpeedFastest
	case level <= 5:
		return zstd.SpeedDefault
	case level <= 8:
		return zstd.SpeedBetterCompression
	default:
		return zstd.SpeedBestCompression
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
