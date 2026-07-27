package s3

import (
	"bytes"
	"context"
	"io"
	"net"
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
