package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			io.Copy(w, rc)
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
