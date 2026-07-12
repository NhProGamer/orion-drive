// Package s3 implements the Handler interface on top of Amazon S3 and any
// S3-compatible service (MinIO, Ceph, Backblaze B2, ...). Downloads are served
// directly from the provider via presigned GET URLs.
package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func init() {
	driver.Register(model.PolicyTypeS3, New)
}

// settings is the S3-specific configuration carried in StoragePolicy.Settings.
type settings struct {
	Region    string `json:"region"`
	PathStyle bool   `json:"path_style"` // required by most S3-compatible servers
}

// Driver stores objects in an S3 bucket.
type Driver struct {
	client   *s3.Client
	presign  *s3.PresignClient
	bucket   string
	basePath string
}

// New builds an S3 driver from a storage policy.
func New(p *model.StoragePolicy) (driver.Handler, error) {
	if p.BucketName == "" {
		return nil, fmt.Errorf("s3: bucket name is required")
	}
	var cfg settings
	if err := p.Settings.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("s3: invalid settings: %w", err)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	opts := s3.Options{
		Region:       cfg.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(p.AccessKey, p.SecretKey, ""),
		UsePathStyle: cfg.PathStyle,
	}
	if p.Server != "" {
		opts.BaseEndpoint = aws.String(p.Server)
	}
	client := s3.New(opts)

	return &Driver{
		client:   client,
		presign:  s3.NewPresignClient(client),
		bucket:   p.BucketName,
		basePath: p.BasePath,
	}, nil
}

// Name returns the backend type.
func (d *Driver) Name() string { return model.PolicyTypeS3 }

// Capabilities reports that downloads are served directly by the provider.
func (d *Driver) Capabilities() *driver.Capabilities {
	return &driver.Capabilities{LocalServe: false}
}

// key maps a backend-relative src to the object key (basePath prefix + src).
func (d *Driver) key(src string) string {
	return strings.TrimPrefix(path.Join(d.basePath, src), "/")
}

// Put uploads an object using the multipart-capable manager.
func (d *Driver) Put(ctx context.Context, src string, r io.Reader, size int64) error {
	uploader := manager.NewUploader(d.client)
	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(d.key(src)),
		Body:   r,
	})
	if err != nil {
		return fmt.Errorf("s3: upload %q: %w", src, err)
	}
	return nil
}

// Source returns a presigned GET URL so the client downloads straight from S3.
func (d *Driver) Source(ctx context.Context, src string, opts driver.SourceOptions) (string, error) {
	expire := opts.Expire
	if expire <= 0 {
		expire = time.Hour
	}
	in := &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(d.key(src)),
	}
	if opts.DownloadFilename != "" {
		in.ResponseContentDisposition = aws.String(
			fmt.Sprintf("attachment; filename*=UTF-8''%s", urlEscape(opts.DownloadFilename)),
		)
	}
	req, err := d.presign.PresignGetObject(ctx, in, s3.WithPresignExpires(expire))
	if err != nil {
		return "", fmt.Errorf("s3: presign %q: %w", src, err)
	}
	return req.URL, nil
}

// Open fetches the object and buffers it into a seekable reader. Downloads use
// Source (presigned URLs) so this is a fallback for internal consumers.
func (d *Driver) Open(ctx context.Context, src string) (driver.ReadSeekCloser, error) {
	out, err := d.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(d.key(src)),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: get %q: %w", src, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, err
	}
	return nopCloser{bytes.NewReader(data)}, nil
}

// Delete removes objects in a single batch request.
func (d *Driver) Delete(ctx context.Context, srcs ...string) ([]string, error) {
	if len(srcs) == 0 {
		return nil, nil
	}
	objects := make([]types.ObjectIdentifier, 0, len(srcs))
	for _, src := range srcs {
		objects = append(objects, types.ObjectIdentifier{Key: aws.String(d.key(src))})
	}
	out, err := d.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(d.bucket),
		Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return srcs, fmt.Errorf("s3: delete: %w", err)
	}
	var failed []string
	for _, e := range out.Errors {
		if e.Key != nil {
			failed = append(failed, aws.ToString(e.Key))
		}
	}
	if len(failed) > 0 {
		return failed, fmt.Errorf("s3: failed to delete %d object(s)", len(failed))
	}
	return nil, nil
}

// nopCloser adds a no-op Close to a *bytes.Reader.
type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

// urlEscape percent-encodes a filename for a Content-Disposition header.
func urlEscape(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || strings.IndexByte("-_.~", r) >= 0 {
			b.WriteByte(r)
		} else {
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	return b.String()
}
