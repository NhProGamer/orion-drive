// Package driver defines the storage-backend abstraction and its registry.
// Each backend (local, s3, remote, ...) implements Handler; a policy selects
// which one is used.
package driver

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/NhProGamer/orion-drive/model"
)

// ReadSeekCloser is an io.Reader that also seeks and closes (for HTTP range
// serving of downloads).
type ReadSeekCloser interface {
	io.ReadSeeker
	io.Closer
}

// SourceOptions tunes a direct download URL.
type SourceOptions struct {
	// Expire is how long the generated URL stays valid.
	Expire time.Duration
	// DownloadFilename, when set, makes the URL force a download with this name
	// (Content-Disposition: attachment).
	DownloadFilename string
}

// Capabilities describes what a backend can do, so higher layers can adapt.
type Capabilities struct {
	// LocalServe is true when the backend exposes files on the local disk and
	// downloads are streamed by OrionDrive itself (vs. a redirect to a signed URL).
	LocalServe bool
}

// Handler is the contract every storage backend implements.
type Handler interface {
	// Name returns the backend type (e.g. "local").
	Name() string
	// Put stores size bytes read from r at the backend path src.
	Put(ctx context.Context, src string, r io.Reader, size int64) error
	// Open returns a seekable reader for the object at src.
	Open(ctx context.Context, src string) (ReadSeekCloser, error)
	// Delete removes one or more objects, returning the paths it failed to delete.
	Delete(ctx context.Context, srcs ...string) ([]string, error)
	// Source returns a direct URL to fetch the object (e.g. a presigned S3 GET),
	// letting the client download straight from the provider. It returns "" when
	// the backend has no direct URL and content must be streamed via Open.
	Source(ctx context.Context, src string, opts SourceOptions) (string, error)
	// Capabilities reports backend features.
	Capabilities() *Capabilities
}

// Factory builds a Handler from a storage policy.
type Factory func(policy *model.StoragePolicy) (Handler, error)

var registry = map[string]Factory{}

// Register makes a backend factory available under a policy type. It is meant
// to be called from driver package init functions.
func Register(policyType string, f Factory) {
	registry[policyType] = f
}

// New builds the Handler for the given policy.
func New(policy *model.StoragePolicy) (Handler, error) {
	f, ok := registry[policy.Type]
	if !ok {
		return nil, fmt.Errorf("no storage driver registered for policy type %q", policy.Type)
	}
	return f(policy)
}
