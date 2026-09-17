// Package archive reads archive files (ZIP, TAR, TAR.GZ and 7-Zip): it lists
// their entries and extracts them. Creating archives is ZIP-only and lives in
// the file manager.
//
// Everything operates on the caller's reader, never a local path: ZIP and 7z
// need random access and get an io.ReaderAt, which over ranged storage means
// only the bytes actually looked at are fetched; the TAR family is read as a
// stream, once. Nothing is copied to disk — a 50 GiB archive used to be
// downloaded in full just to list its index.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	"github.com/bodgit/sevenzip"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Entry describes one member of an archive.
type Entry struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir"`
	// Unsupported marks a member that exists in the archive but has no
	// equivalent in the drive — a symlink, a device, a FIFO. It is reported so
	// callers can tell the user what was left out instead of writing an empty
	// file in its place.
	Unsupported bool `json:"unsupported,omitempty"`
}

// Supported archive formats.
const (
	FormatZip    = "zip"
	FormatTar    = "tar"
	FormatTarGz  = "tar.gz"
	FormatTarBz2 = "tar.bz2"
	FormatTarXz  = "tar.xz"
	FormatTarZst = "tar.zst"
	Format7z     = "7z"
)

// suffixes maps a file extension to its format, longest first so ".tar.gz" is
// not mistaken for ".gz" of nothing.
var suffixes = []struct {
	ext    string
	format string
}{
	{".tar.gz", FormatTarGz},
	{".tgz", FormatTarGz},
	{".tar.bz2", FormatTarBz2},
	{".tbz2", FormatTarBz2},
	{".tbz", FormatTarBz2},
	{".tar.xz", FormatTarXz},
	{".txz", FormatTarXz},
	{".tar.zst", FormatTarZst},
	{".tzst", FormatTarZst},
	{".tar", FormatTar},
	{".zip", FormatZip},
	{".7z", Format7z},
}

// Format detects an archive format from a file name, or "" if unsupported.
func Format(name string) string {
	l := strings.ToLower(name)
	for _, s := range suffixes {
		if strings.HasSuffix(l, s.ext) {
			return s.format
		}
	}
	return ""
}

// IsArchive reports whether name looks like a supported archive.
func IsArchive(name string) bool { return Format(name) != "" }

// SanitizePath normalises an entry name into a relative, slash-separated path,
// reporting false for a name nothing can safely be written under.
//
// Archives carry whatever the tool that made them wrote: Windows separators,
// drive letters, absolute paths, "..". Callers must not pass any of that
// through — and must not simply reject the whole archive over one odd name
// either, which is what happens when a backslash reaches the file manager's
// name validation.
func SanitizePath(name string) (string, bool) {
	clean := strings.ReplaceAll(name, "\\", "/")
	// "C:/x" and "//server/share/x" become plain relative paths.
	if i := strings.Index(clean, ":"); i >= 0 && !strings.Contains(clean[:i], "/") {
		clean = clean[i+1:]
	}
	clean = strings.Trim(path.Clean("/"+clean), "/")
	if clean == "" || clean == "." {
		return "", false
	}
	// path.Clean on an absolute path already resolved away every "..", so one
	// surviving here means the name was nothing but traversal.
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", false
		}
	}
	return clean, true
}

// Source is an archive's bytes. Seek is what the central-directory formats
// need; the TAR family only ever reads forward.
type Source interface {
	io.ReadSeeker
}

// Visit is called for each member. For files, open() yields a reader the
// callback must close; for directories and unsupported members open is nil.
type Visit func(e Entry, open func() (io.ReadCloser, error)) error

// List returns the entries of an archive named name, read from src.
func List(src Source, size int64, name string) ([]Entry, error) {
	var entries []Entry
	err := Extract(src, size, name, func(e Entry, open func() (io.ReadCloser, error)) error {
		entries = append(entries, e)
		return nil
	})
	return entries, err
}

// Extract iterates an archive named name (the name selects the format), read
// from src. size is the archive's length, which the central-directory formats
// need to locate their index.
func Extract(src Source, size int64, name string, fn Visit) error {
	format := Format(name)
	switch format {
	case FormatZip:
		return walkZip(src, size, fn)
	case FormatTar, FormatTarGz, FormatTarBz2, FormatTarXz, FormatTarZst:
		return walkTar(src, format, fn)
	case Format7z:
		return walk7z(src, size, fn)
	default:
		return fmt.Errorf("archive: unsupported format for %q", name)
	}
}

// readerAt adapts a Source to io.ReaderAt for the readers that need random
// access. Seek-then-read rather than a real positional read, so it is not safe
// for concurrent use — which matches how the readers use it: one entry at a
// time.
type readerAt struct {
	src Source
	mu  sync.Mutex
}

func (r *readerAt) ReadAt(p []byte, off int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.src.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	return io.ReadFull(r.src, p)
}

func walkZip(src Source, size int64, fn Visit) error {
	r, err := zip.NewReader(&readerAt{src: src}, size)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		info := f.FileInfo()
		// A ZIP written on a Unix system can carry symlinks and device nodes in
		// its mode bits; extracting one as a file would silently produce a
		// document whose content is the link target.
		if mode := info.Mode(); !info.IsDir() && !mode.IsRegular() {
			if err := fn(Entry{Name: f.Name, Size: info.Size(), Unsupported: true}, nil); err != nil {
				return err
			}
			continue
		}
		e := Entry{Name: f.Name, Size: info.Size(), IsDir: info.IsDir()}
		open := func() (io.ReadCloser, error) { return f.Open() }
		if e.IsDir {
			open = nil
		}
		if err := fn(e, open); err != nil {
			return err
		}
	}
	return nil
}

func walk7z(src Source, size int64, fn Visit) error {
	r, err := sevenzip.NewReader(&readerAt{src: src}, size)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		info := f.FileInfo()
		if mode := info.Mode(); !info.IsDir() && !mode.IsRegular() {
			if err := fn(Entry{Name: f.Name, Size: info.Size(), Unsupported: true}, nil); err != nil {
				return err
			}
			continue
		}
		e := Entry{Name: f.Name, Size: info.Size(), IsDir: info.IsDir()}
		open := func() (io.ReadCloser, error) { return f.Open() }
		if e.IsDir {
			open = nil
		}
		if err := fn(e, open); err != nil {
			return err
		}
	}
	return nil
}

func walkTar(src Source, format string, fn Visit) error {
	body, closer, err := decompress(src, format)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}

	tr := tar.NewReader(body)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		isDir := hdr.FileInfo().IsDir()
		// A tar also carries symlinks, hardlinks, devices and FIFOs. None of
		// them has an equivalent in the drive, and passing them on as regular
		// entries produced empty files: skip them and let the caller count what
		// it did not get.
		if !isDir && !regularTarEntry(hdr) {
			if err := fn(Entry{Name: hdr.Name, Size: hdr.Size, Unsupported: true}, nil); err != nil {
				return err
			}
			continue
		}
		e := Entry{Name: hdr.Name, Size: hdr.Size, IsDir: isDir}
		// tr is only valid until the next Next(); expose it read-only.
		open := func() (io.ReadCloser, error) { return io.NopCloser(tr), nil }
		if isDir {
			open = nil
		}
		if err := fn(e, open); err != nil {
			return err
		}
	}
}

// decompress wraps a TAR stream in the decompressor its format calls for. The
// returned closer, when non-nil, must be closed by the caller.
func decompress(src io.Reader, format string) (io.Reader, io.Closer, error) {
	switch format {
	case FormatTar:
		return src, nil, nil
	case FormatTarGz:
		zr, err := gzip.NewReader(src)
		if err != nil {
			return nil, nil, err
		}
		return zr, zr, nil
	case FormatTarBz2:
		// The standard library reads bzip2 but cannot write it, which is why it
		// is not offered as a creation format.
		return bzip2.NewReader(src), nil, nil
	case FormatTarXz:
		xr, err := xz.NewReader(src)
		if err != nil {
			return nil, nil, err
		}
		return xr, nil, nil
	case FormatTarZst:
		zr, err := zstd.NewReader(src)
		if err != nil {
			return nil, nil, err
		}
		return zr.IOReadCloser(), zr.IOReadCloser(), nil
	default:
		return nil, nil, fmt.Errorf("archive: %q is not a TAR stream", format)
	}
}

// regularTarEntry reports whether a header describes plain file content.
func regularTarEntry(hdr *tar.Header) bool {
	switch hdr.Typeflag {
	case tar.TypeReg, tar.TypeRegA:
		return true
	default:
		return false
	}
}
