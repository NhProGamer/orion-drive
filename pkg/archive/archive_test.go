package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

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
	err := Extract(path, func(e Entry, open func() (io.ReadCloser, error)) error {
		entries = append(entries, e)
		if open != nil {
			rc, err := open()
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
	list, err := List(p)
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
	if _, err := List("foo.rar"); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}
