package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	_ "github.com/NhProGamer/orion-drive/pkg/filemanager/driver/local" // register the local backend
	"github.com/NhProGamer/orion-drive/pkg/slaveauth"
)

const testSecret = "shared-secret"

// fakeSlave stands in for a slave node: it verifies signatures and stores bytes
// on a local backend, mirroring routers/controllers/slave.go.
func fakeSlave(t *testing.T, store driver.Handler) http.Handler {
	t.Helper()
	guard := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if err := slaveauth.Verify(testSecret, r.Method, r.URL.Path, r.URL.Query()); err != nil {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc(EndpointUpload, guard(func(w http.ResponseWriter, r *http.Request) {
		if err := store.Put(r.Context(), r.URL.Query().Get(ParamPath), r.Body, r.ContentLength); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	serve := func(attachment bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			rc, err := store.Open(r.Context(), r.URL.Query().Get(ParamPath))
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			defer rc.Close()
			if attachment {
				w.Header().Set("Content-Disposition", "attachment; filename="+r.URL.Query().Get(ParamName))
			}
			// Mirrors the real slave (routers/controllers/slave.go), which serves
			// through ServeContent and therefore answers Range requests — what
			// the driver's ranged reader relies on.
			http.ServeContent(w, r, r.URL.Query().Get(ParamPath), time.Time{}, rc)
		}
	}
	mux.HandleFunc(EndpointContent, guard(serve(false)))
	mux.HandleFunc(EndpointDownload, guard(serve(true)))
	mux.HandleFunc(EndpointDelete, guard(func(w http.ResponseWriter, r *http.Request) {
		var req deleteRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		failed, _ := store.Delete(r.Context(), req.Paths...)
		_ = json.NewEncoder(w).Encode(deleteResponse{Failed: failed})
	}))
	return mux
}

func newDriver(t *testing.T) (*Driver, string) {
	t.Helper()
	store, err := driver.New(&model.StoragePolicy{Type: model.PolicyTypeLocal, BasePath: t.TempDir()})
	if err != nil {
		t.Fatalf("local store: %v", err)
	}
	srv := httptest.NewServer(fakeSlave(t, store))
	t.Cleanup(srv.Close)

	d, err := New(&model.StoragePolicy{Type: model.PolicyTypeRemote, Server: srv.URL, SecretKey: testSecret})
	if err != nil {
		t.Fatalf("remote driver: %v", err)
	}
	return d.(*Driver), srv.URL
}

func TestRemotePutOpenDelete(t *testing.T) {
	d, _ := newDriver(t)
	ctx := context.Background()
	const src, content = "u1/hello.txt", "hello slave"

	if err := d.Put(ctx, src, strings.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, err := d.Open(ctx, src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != content {
		t.Fatalf("Open: got %q, want %q", got, content)
	}

	if failed, err := d.Delete(ctx, src); err != nil || len(failed) != 0 {
		t.Fatalf("Delete: failed=%v err=%v", failed, err)
	}
	if _, err := d.Open(ctx, src); err == nil {
		t.Fatal("Open after Delete: expected error")
	}
}

func TestRemoteSourceDirectDownload(t *testing.T) {
	d, _ := newDriver(t)
	ctx := context.Background()
	const src, content = "u1/doc.txt", "direct content"
	if err := d.Put(ctx, src, strings.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("Put: %v", err)
	}

	url, err := d.Source(ctx, src, driver.SourceOptions{DownloadFilename: "doc.txt"})
	if err != nil || url == "" {
		t.Fatalf("Source: url=%q err=%v", url, err)
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET signed url: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != content {
		t.Fatalf("body: got %q, want %q", body, content)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("Content-Disposition: %q", cd)
	}
}

func TestRemoteRejectsUnsigned(t *testing.T) {
	_, base := newDriver(t)
	resp, err := http.Get(base + EndpointContent + "?" + ParamPath + "=whatever")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unsigned request: got %d, want 403", resp.StatusCode)
	}
}

func TestRemoteOpenReadsByRanges(t *testing.T) {
	d, _ := newDriver(t)
	ctx := context.Background()

	data := make([]byte, 200<<10)
	for i := range data {
		data[i] = byte('a' + i%26)
	}
	const src = "ranged/obj.bin"
	if err := d.Put(ctx, src, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := d.Open(ctx, src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()

	// Reading it whole must match, byte for byte.
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("read %d bytes, want %d identical", len(got), len(data))
	}

	// Seeking from the end is how an archive reader finds a central directory:
	// it has to work without the whole object having been read first.
	if _, err := rc.Seek(-16, io.SeekEnd); err != nil {
		t.Fatalf("seek end: %v", err)
	}
	tail := make([]byte, 16)
	if _, err := io.ReadFull(rc, tail); err != nil {
		t.Fatalf("read tail: %v", err)
	}
	if !bytes.Equal(tail, data[len(data)-16:]) {
		t.Fatalf("tail mismatch")
	}

	// And a jump backwards lands on the right bytes.
	if _, err := rc.Seek(1000, io.SeekStart); err != nil {
		t.Fatalf("seek back: %v", err)
	}
	mid := make([]byte, 8)
	if _, err := io.ReadFull(rc, mid); err != nil {
		t.Fatalf("read mid: %v", err)
	}
	if !bytes.Equal(mid, data[1000:1008]) {
		t.Fatalf("mid mismatch: %q", mid)
	}
}

func TestRemoteOpenRefusesASlaveThatIgnoresRanges(t *testing.T) {
	// A slave that answered 200 with the whole object to a ranged request would
	// feed the caller bytes from the wrong offset; the driver must refuse rather
	// than corrupt what it returns.
	store, err := driver.New(&model.StoragePolicy{Type: model.PolicyTypeLocal, BasePath: t.TempDir()})
	if err != nil {
		t.Fatalf("local store: %v", err)
	}
	ctx := context.Background()
	data := bytes.Repeat([]byte("x"), 4096)
	if err := store.Put(ctx, "obj", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := slaveauth.Verify(testSecret, r.Method, r.URL.Path, r.URL.Query()); err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK) // range ignored on purpose
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	d, err := New(&model.StoragePolicy{Type: model.PolicyTypeRemote, Server: srv.URL, SecretKey: testSecret})
	if err != nil {
		t.Fatalf("remote driver: %v", err)
	}
	rc, err := d.Open(ctx, "obj")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()

	if _, err := io.ReadFull(rc, make([]byte, 8)); err != nil {
		t.Fatalf("first read: %v", err)
	}
	// Far enough that the reader must re-request rather than skip ahead.
	if _, err := rc.Seek(3<<20, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if _, err := rc.Read(make([]byte, 8)); err == nil {
		t.Fatal("expected the ignored range to be rejected")
	}
}
