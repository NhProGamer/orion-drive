// Package archive reads archive files (ZIP, TAR, TAR.GZ and 7-Zip): it lists
// their entries and extracts them. Creating archives is ZIP-only and lives in
// the file manager. Everything operates on a local file path so large archives
// stay off-heap.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/bodgit/sevenzip"
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
	FormatZip   = "zip"
	FormatTar   = "tar"
	FormatTarGz = "tar.gz"
	Format7z    = "7z"
)

// Format detects an archive format from a file name, or "" if unsupported.
func Format(name string) string {
	l := strings.ToLower(name)
	switch {
	case strings.HasSuffix(l, ".zip"):
		return FormatZip
	case strings.HasSuffix(l, ".tar.gz"), strings.HasSuffix(l, ".tgz"):
		return FormatTarGz
	case strings.HasSuffix(l, ".tar"):
		return FormatTar
	case strings.HasSuffix(l, ".7z"):
		return Format7z
	default:
		return ""
	}
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

// List returns the entries of the archive at path.
func List(path string) ([]Entry, error) {
	var entries []Entry
	err := walk(path, func(e Entry, _ func() (io.ReadCloser, error)) error {
		entries = append(entries, e)
		return nil
	})
	return entries, err
}

// Extract iterates the archive at path, invoking fn for each entry. For files,
// open() yields a reader the callback must close; for directories open is nil.
func Extract(path string, fn func(e Entry, open func() (io.ReadCloser, error)) error) error {
	return walk(path, fn)
}

// walk dispatches on format and drives the per-entry callback.
func walk(path string, fn func(e Entry, open func() (io.ReadCloser, error)) error) error {
	switch Format(path) {
	case FormatZip:
		return walkZip(path, fn)
	case FormatTar:
		return walkTar(path, false, fn)
	case FormatTarGz:
		return walkTar(path, true, fn)
	case Format7z:
		return walk7z(path, fn)
	default:
		return fmt.Errorf("archive: unsupported format for %q", path)
	}
}

func walkZip(path string, fn func(Entry, func() (io.ReadCloser, error)) error) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
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

func walk7z(path string, fn func(Entry, func() (io.ReadCloser, error)) error) error {
	r, err := sevenzip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
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

func walkTar(path string, gz bool, fn func(Entry, func() (io.ReadCloser, error)) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var src io.Reader = f
	if gz {
		zr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer zr.Close()
		src = zr
	}

	tr := tar.NewReader(src)
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

// regularTarEntry reports whether a header describes plain file content.
func regularTarEntry(hdr *tar.Header) bool {
	switch hdr.Typeflag {
	case tar.TypeReg, tar.TypeRegA:
		return true
	default:
		return false
	}
}
