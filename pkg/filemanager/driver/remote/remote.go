// Package remote implements the Handler interface against a slave OrionDrive
// node over HTTP. The master signs every request with a shared secret; the slave
// stores the bytes on its own disk. Downloads are handed to the client as signed
// direct URLs so the transfer goes straight from the slave, not through the master.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/NhProGamer/orion-drive/pkg/slaveauth"
)

func init() {
	driver.Register(model.PolicyTypeRemote, New)
}

// Driver talks to a slave node's storage HTTP API.
type Driver struct {
	base     string // slave base URL without a trailing slash
	secret   string // shared signing secret
	basePath string // optional key prefix within the slave storage
	client   *http.Client
}

// New builds a remote driver from a storage policy. Server is the slave base URL
// and SecretKey is the shared signing secret.
func New(p *model.StoragePolicy) (driver.Handler, error) {
	if p.Server == "" {
		return nil, fmt.Errorf("remote: server URL is required")
	}
	if p.SecretKey == "" {
		return nil, fmt.Errorf("remote: slave secret is required")
	}
	return &Driver{
		base:     strings.TrimRight(p.Server, "/"),
		secret:   p.SecretKey,
		basePath: p.BasePath,
		// Bound connect/TLS/response-header waits so a stalled slave can't hang a
		// request indefinitely, without a blanket client Timeout that would kill
		// legitimately long uploads/downloads mid-body.
		client: &http.Client{
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}, nil
}

// Name returns the backend type.
func (d *Driver) Name() string { return model.PolicyTypeRemote }

// Capabilities reports that downloads are served directly by the slave node.
func (d *Driver) Capabilities() *driver.Capabilities {
	return &driver.Capabilities{LocalServe: false}
}

// key maps a backend-relative src to the slave object key (basePath prefix + src).
func (d *Driver) key(src string) string {
	return strings.TrimPrefix(path.Join(d.basePath, src), "/")
}

// signedURL builds a signed URL for endpoint with the given query values.
func (d *Driver) signedURL(method, endpoint string, q url.Values, ttl time.Duration) string {
	q = slaveauth.SignValues(d.secret, method, endpoint, q, ttl)
	return d.base + endpoint + "?" + q.Encode()
}

// Put streams size bytes to the slave.
func (d *Driver) Put(ctx context.Context, src string, r io.Reader, size int64) error {
	q := url.Values{}
	q.Set(ParamPath, d.key(src))
	u := d.signedURL(http.MethodPost, EndpointUpload, q, time.Hour)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, r)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("remote: upload %q: %w", src, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("remote: upload %q: %s", src, statusError(resp))
	}
	return nil
}

// Source returns a signed direct-download URL on the slave, so the client
// downloads straight from the slave node.
func (d *Driver) Source(ctx context.Context, src string, opts driver.SourceOptions) (string, error) {
	ttl := opts.Expire
	if ttl <= 0 {
		ttl = time.Hour
	}
	q := url.Values{}
	q.Set(ParamPath, d.key(src))
	if opts.DownloadFilename != "" {
		q.Set(ParamName, opts.DownloadFilename)
	}
	return d.signedURL(http.MethodGet, EndpointDownload, q, ttl), nil
}

// Open fetches the object and buffers it into a seekable reader. Downloads use
// Source (signed URLs); this is a fallback for internal consumers.
func (d *Driver) Open(ctx context.Context, src string) (driver.ReadSeekCloser, error) {
	q := url.Values{}
	q.Set(ParamPath, d.key(src))
	u := d.signedURL(http.MethodGet, EndpointContent, q, time.Hour)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote: open %q: %w", src, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("remote: open %q: %s", src, statusError(resp))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return nopCloser{bytes.NewReader(data)}, nil
}

// Delete removes objects on the slave, returning the keys it failed to delete.
func (d *Driver) Delete(ctx context.Context, srcs ...string) ([]string, error) {
	if len(srcs) == 0 {
		return nil, nil
	}
	keys := make([]string, len(srcs))
	bySrc := make(map[string]string, len(srcs))
	for i, src := range srcs {
		keys[i] = d.key(src)
		bySrc[keys[i]] = src
	}
	body, _ := json.Marshal(deleteRequest{Paths: keys})
	u := d.signedURL(http.MethodPost, EndpointDelete, url.Values{}, time.Hour)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return srcs, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return srcs, fmt.Errorf("remote: delete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return srcs, fmt.Errorf("remote: delete: %s", statusError(resp))
	}
	var out deleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, nil // deleted, response body just unreadable
	}
	var failed []string
	for _, k := range out.Failed {
		if src, ok := bySrc[k]; ok {
			failed = append(failed, src)
		} else {
			failed = append(failed, k)
		}
	}
	if len(failed) > 0 {
		return failed, fmt.Errorf("remote: failed to delete %d object(s)", len(failed))
	}
	return nil, nil
}

// deleteRequest and deleteResponse are the JSON payloads of EndpointDelete.
type deleteRequest struct {
	Paths []string `json:"paths"`
}

type deleteResponse struct {
	Failed []string `json:"failed"`
}

// statusError summarises a non-2xx response for error messages.
func statusError(resp *http.Response) string {
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if len(msg) > 0 {
		return fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return resp.Status
}

// nopCloser adds a no-op Close to a *bytes.Reader.
type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }
