// Package local implements the Handler interface on the local filesystem.
package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
)

func init() {
	driver.Register(model.PolicyTypeLocal, func(p *model.StoragePolicy) (driver.Handler, error) {
		base := p.BasePath
		if base == "" {
			base = "data/storage"
		}
		abs, err := filepath.Abs(base)
		if err != nil {
			return nil, err
		}
		return &Driver{base: abs}, nil
	})
}

// Driver stores objects under a base directory on the local disk.
type Driver struct{ base string }

// Name returns the backend type.
func (d *Driver) Name() string { return model.PolicyTypeLocal }

// Capabilities reports that files are served locally.
func (d *Driver) Capabilities() *driver.Capabilities {
	return &driver.Capabilities{LocalServe: true}
}

// resolve turns a backend-relative src into an absolute path, guarding against
// path traversal outside the base directory.
func (d *Driver) resolve(src string) (string, error) {
	clean := filepath.Clean("/" + strings.ReplaceAll(src, "\\", "/"))
	abs := filepath.Join(d.base, clean)
	if abs != d.base && !strings.HasPrefix(abs, d.base+string(os.PathSeparator)) {
		return "", fmt.Errorf("local: path %q escapes base", src)
	}
	return abs, nil
}

// Put writes r to the object path src, creating parent directories.
func (d *Driver) Put(ctx context.Context, src string, r io.Reader, size int64) error {
	abs, err := d.resolve(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	f, err := os.Create(abs)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		_ = os.Remove(abs)
		return err
	}
	return nil
}

// Source has no direct URL for local storage; downloads are streamed by
// OrionDrive itself via Open.
func (d *Driver) Source(ctx context.Context, src string, opts driver.SourceOptions) (string, error) {
	return "", nil
}

// Open returns a seekable reader for src.
func (d *Driver) Open(ctx context.Context, src string) (driver.ReadSeekCloser, error) {
	abs, err := d.resolve(src)
	if err != nil {
		return nil, err
	}
	return os.Open(abs)
}

// Delete removes the given objects, collecting any that could not be deleted.
func (d *Driver) Delete(ctx context.Context, srcs ...string) ([]string, error) {
	var failed []string
	for _, src := range srcs {
		abs, err := d.resolve(src)
		if err != nil {
			failed = append(failed, src)
			continue
		}
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			failed = append(failed, src)
		}
	}
	if len(failed) > 0 {
		return failed, fmt.Errorf("local: failed to delete %d object(s)", len(failed))
	}
	return nil, nil
}
