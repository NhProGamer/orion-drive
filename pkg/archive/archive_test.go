package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// openFixture turns a fixture path into the reader the package takes, with its
// size.
func openFixture(t *testing.T, p string) (*os.File, int64) {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open %s: %v", p, err)
	}
	t.Cleanup(func() { f.Close() })
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}
	return f, info.Size()
}

func writeZip(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "a.zip")
	f, _ := os.Create(p)
	defer f.Close()
	zw := zip.NewWriter(f)
	w, _ := zw.Create("hello.txt")
	_, _ = w.Write([]byte("hello"))
	w, _ = zw.Create("dir/nested.txt")
	_, _ = w.Write([]byte("nested-content"))
	_, _ = zw.Create("empty/")
	zw.Close()
	return p
}

func writeTarGz(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "a.tar.gz")
	f, _ := os.Create(p)
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte("tar-body")
	_ = tw.WriteHeader(&tar.Header{Name: "file.txt", Size: int64(len(body)), Mode: 0o644})
	_, _ = tw.Write(body)
	tw.Close()
	gz.Close()
	return p
}

func collect(t *testing.T, path string) (map[string]string, []Entry) {
	t.Helper()
	contents := map[string]string{}
	var entries []Entry
	src, size := openFixture(t, path)
	err := Extract(src, size, path, func(e Entry, body func() (io.ReadCloser, error)) error {
		entries = append(entries, e)
		if body != nil {
			rc, err := body()
			if err != nil {
				return err
			}
			defer rc.Close()
			b, _ := io.ReadAll(rc)
			contents[e.Name] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return contents, entries
}

func TestZip(t *testing.T) {
	p := writeZip(t, t.TempDir())
	if got := Format(p); got != FormatZip {
		t.Fatalf("format = %s", got)
	}
	src, size := openFixture(t, p)
	list, err := List(src, size, p)
	if err != nil || len(list) != 3 {
		t.Fatalf("list = %v err=%v", list, err)
	}
	contents, _ := collect(t, p)
	if contents["hello.txt"] != "hello" || contents["dir/nested.txt"] != "nested-content" {
		t.Fatalf("contents = %v", contents)
	}
}

func TestTarGz(t *testing.T) {
	p := writeTarGz(t, t.TempDir())
	if got := Format(p); got != FormatTarGz {
		t.Fatalf("format = %s", got)
	}
	contents, entries := collect(t, p)
	if len(entries) != 1 || contents["file.txt"] != "tar-body" {
		t.Fatalf("entries=%v contents=%v", entries, contents)
	}
}

func TestUnsupported(t *testing.T) {
	if Format("foo.rar") != "" {
		t.Fatal("rar should be unsupported")
	}
	if _, err := List(bytes.NewReader(nil), 0, "foo.rar"); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestSanitizePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"notes.txt", "notes.txt", true},
		{"dir/file.txt", "dir/file.txt", true},
		// Windows separators and drive letters: what old archivers wrote.
		{`dir\file.txt`, "dir/file.txt", true},
		{`C:\Users\x\notes.txt`, "Users/x/notes.txt", true},
		{`\\server\share\f.txt`, "server/share/f.txt", true},
		// Traversal resolves away instead of failing the whole archive.
		{"../escaped.txt", "escaped.txt", true},
		{`..\..\escaped.txt`, "escaped.txt", true},
		{"a/../b.txt", "b.txt", true},
		{"/absolute.txt", "absolute.txt", true},
		{"./rel.txt", "rel.txt", true},
		// Nothing can be written under these.
		{"", "", false},
		{".", "", false},
		{"..", "", false},
		{"../..", "", false},
		{"/", "", false},
	}
	for _, c := range cases {
		got, ok := SanitizePath(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("SanitizePath(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestTarSkipsEntriesWithNoFileContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "links.tar")
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tw := tar.NewWriter(f)
	write := func(hdr *tar.Header, body string) {
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("header %s: %v", hdr.Name, err)
		}
		if body != "" {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatalf("body %s: %v", hdr.Name, err)
			}
		}
	}
	write(&tar.Header{Name: "real.txt", Typeflag: tar.TypeReg, Size: 4, Mode: 0o644}, "body")
	write(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "real.txt", Mode: 0o777}, "")
	write(&tar.Header{Name: "dev", Typeflag: tar.TypeChar, Mode: 0o644}, "")
	write(&tar.Header{Name: "dir/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	tw.Close()
	f.Close()

	var opened, unsupported, dirs int
	src, size := openFixture(t, p)
	err = Extract(src, size, p, func(e Entry, body func() (io.ReadCloser, error)) error {
		switch {
		case e.Unsupported:
			unsupported++
			if body != nil {
				t.Errorf("%q is unsupported but was offered a reader", e.Name)
			}
		case e.IsDir:
			dirs++
		default:
			opened++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// A symlink and a device node have no equivalent in the drive, and must be
	// reported as such rather than handed over as empty files.
	if opened != 1 || unsupported != 2 || dirs != 1 {
		t.Fatalf("files=%d unsupported=%d dirs=%d, want 1/2/1", opened, unsupported, dirs)
	}
}
