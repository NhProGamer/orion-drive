// Package statics embeds the built frontend SPA and serves it, with SPA
// fallback (unknown non-API paths return index.html).
package statics

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// dist holds the built frontend. The directory must exist at build time, so a
// placeholder file is committed; a real build overwrites it.
//
//go:embed all:dist
var dist embed.FS

// Register mounts the SPA on the engine. Requests under /api are left untouched.
func Register(r *gin.Engine) error {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return err
	}

	// Prefer an on-disk build (data/statics) when present, for fast dev iteration.
	var serveFS http.FileSystem = http.FS(sub)
	if _, err := os.Stat("data/statics/index.html"); err == nil {
		serveFS = http.Dir("data/statics")
	}

	index, err := readIndex(serveFS)
	if err != nil {
		// No build yet: serve a helpful placeholder instead of failing to boot.
		index = []byte(placeholder)
	}

	fileServer := http.FileServer(serveFS)
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"code": 40400, "msg": "not found"})
			return
		}
		if p != "/" && fileExists(serveFS, p) {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
	return nil
}

func readIndex(fsys http.FileSystem) ([]byte, error) {
	f, err := fsys.Open("/index.html")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := f.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}

func fileExists(fsys http.FileSystem, p string) bool {
	f, err := fsys.Open(path.Clean(p))
	if err != nil {
		return false
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && !info.IsDir() {
		return true
	}
	return false
}

const placeholder = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<title>OrionDrive</title></head><body style="font-family:system-ui;background:#1a1524;color:#eee;padding:3rem">` +
	`<h1>OrionDrive backend is running</h1>` +
	`<p>The frontend has not been built yet. Run <code>cd frontend &amp;&amp; npm install &amp;&amp; npm run build</code>, ` +
	`or use the Vite dev server (<code>npm run dev</code>) during development.</p></body></html>`
