package s3

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	testEndpoint = "http://127.0.0.1:19000"
	testAddr     = "127.0.0.1:19000"
	testKey      = "minioadmin"
	testBucket   = "orion-test"
)

// TestS3Driver exercises the S3 backend against a local MinIO. It is skipped
// when no S3-compatible server is reachable (e.g. in CI without MinIO): start
// one with
//
//	podman run -d -p 19000:9000 -e MINIO_ROOT_USER=minioadmin \
//	  -e MINIO_ROOT_PASSWORD=minioadmin minio/minio server /data
func TestS3Driver(t *testing.T) {
	if c, err := net.DialTimeout("tcp", testAddr, 500*time.Millisecond); err != nil {
		t.Skipf("no S3 server at %s: %v", testAddr, err)
	} else {
		_ = c.Close()
	}
	ctx := context.Background()

	// Ensure the bucket exists (ignore "already owned").
	raw := awss3.New(awss3.Options{
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider(testKey, testKey, ""),
		UsePathStyle: true,
		BaseEndpoint: aws.String(testEndpoint),
	})
	_, _ = raw.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String(testBucket)})

	d, err := New(&model.StoragePolicy{
		Type:       model.PolicyTypeS3,
		Server:     testEndpoint,
		BucketName: testBucket,
		AccessKey:  testKey,
		SecretKey:  testKey,
		Settings:   model.MustJSON(map[string]any{"region": "us-east-1", "path_style": true}),
	})
	if err != nil {
		t.Fatalf("build driver: %v", err)
	}

	const src = "test/obj.txt"
	data := []byte("hello s3 world")

	if err := d.Put(ctx, src, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := d.Open(ctx, src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("read back = %q, want %q", got, data)
	}

	// A presigned direct URL is produced for downloads.
	if url, err := d.Source(ctx, src, driver.SourceOptions{Expire: time.Minute}); err != nil || url == "" {
		t.Fatalf("source: url=%q err=%v", url, err)
	}

	if _, err := d.Delete(ctx, src); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := d.Open(ctx, src); err == nil {
		t.Fatalf("open after delete should fail")
	}
}

// fakeS3 serves one object with path-style GETs, honouring Range through
// http.ServeContent — enough to exercise the driver's ranged reader without a
// real S3 server, so this runs everywhere (unlike TestS3Driver above).
func fakeS3(t *testing.T, key string, data []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+testBucket+"/"+key {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.ServeContent(w, r, key, time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fakeS3Driver(t *testing.T, endpoint string) driver.Handler {
	t.Helper()
	d, err := New(&model.StoragePolicy{
		Type:       model.PolicyTypeS3,
		Server:     endpoint,
		BucketName: testBucket,
		AccessKey:  testKey,
		SecretKey:  testKey,
		Settings:   model.MustJSON(map[string]any{"region": "us-east-1", "path_style": true}),
	})
	if err != nil {
		t.Fatalf("build driver: %v", err)
	}
	return d
}

func TestS3OpenReadsByRanges(t *testing.T) {
	data := make([]byte, 150<<10)
	for i := range data {
		data[i] = byte('a' + i%26)
	}
	const key = "ranged/obj.bin"
	d := fakeS3Driver(t, fakeS3(t, key, data).URL)

	rc, err := d.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("read %d bytes, want %d identical", len(got), len(data))
	}

	// Seeking from the end, then back into the middle: the access pattern an
	// archive reader has, and the one that used to require the whole object in
	// memory.
	if _, err := rc.Seek(-32, io.SeekEnd); err != nil {
		t.Fatalf("seek end: %v", err)
	}
	tail := make([]byte, 32)
	if _, err := io.ReadFull(rc, tail); err != nil {
		t.Fatalf("read tail: %v", err)
	}
	if !bytes.Equal(tail, data[len(data)-32:]) {
		t.Fatal("tail mismatch")
	}
	if _, err := rc.Seek(4096, io.SeekStart); err != nil {
		t.Fatalf("seek back: %v", err)
	}
	mid := make([]byte, 16)
	if _, err := io.ReadFull(rc, mid); err != nil {
		t.Fatalf("read mid: %v", err)
	}
	if !bytes.Equal(mid, data[4096:4112]) {
		t.Fatalf("mid mismatch: %q", mid)
	}
}

func TestS3OpenReportsAMissingObject(t *testing.T) {
	d := fakeS3Driver(t, fakeS3(t, "present", []byte("x")).URL)
	if _, err := d.Open(context.Background(), "absent"); err == nil {
		t.Fatal("opening a missing object must fail")
	}
}

func TestObjectSizeReadsTheTotalFromContentRange(t *testing.T) {
	out := &awss3.GetObjectOutput{
		ContentRange:  aws.String("bytes 100-199/4096"),
		ContentLength: aws.Int64(100),
	}
	if got, err := objectSize(out, 100); err != nil || got != 4096 {
		t.Fatalf("objectSize = (%d, %v), want (4096, nil)", got, err)
	}
	// A whole-object answer carries the size as its length.
	whole := &awss3.GetObjectOutput{ContentLength: aws.Int64(4096)}
	if got, err := objectSize(whole, 0); err != nil || got != 4096 {
		t.Fatalf("objectSize = (%d, %v), want (4096, nil)", got, err)
	}
	// A ranged answer without Content-Range leaves no way to know the total,
	// and guessing would corrupt everything read past this point.
	if _, err := objectSize(whole, 100); err == nil {
		t.Fatal("a ranged answer with no Content-Range must be refused")
	}
}
