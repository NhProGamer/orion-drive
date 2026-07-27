package controllers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// safeInlineTypes maps file extensions to a Content-Type safe to serve inline in
// a browser. It deliberately excludes anything script-capable on our own origin
// — no text/html, no image/svg+xml — and forces text to text/plain so a browser
// never renders it as markup. Anything not listed is served as an attachment
// download. Shared by the authenticated download, public direct links and public
// share downloads so all three enforce the same hardening.
var safeInlineTypes = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp",
	".ico": "image/x-icon", ".avif": "image/avif",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm",
	".ogv": "video/ogg", ".mov": "video/quicktime", ".mkv": "video/x-matroska",
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg",
	".oga": "audio/ogg", ".opus": "audio/ogg", ".flac": "audio/flac",
	".m4a": "audio/mp4", ".aac": "audio/aac",
	".pdf": "application/pdf",
	".txt": "text/plain; charset=utf-8", ".md": "text/plain; charset=utf-8",
	".log": "text/plain; charset=utf-8", ".csv": "text/plain; charset=utf-8",
}

// safeInlineType returns a viewer-safe Content-Type for name and whether inline
// serving is permitted for that type (false → force an attachment download).
func safeInlineType(name string) (string, bool) {
	ct, ok := safeInlineTypes[strings.ToLower(path.Ext(name))]
	return ct, ok
}

// serveContent streams a file to the response, either inline (in-browser view)
// or as an attachment. Inline is honoured only for viewer-safe content types
// (safeInlineType) and always carries X-Content-Type-Options: nosniff, so no
// caller — authenticated, direct-link or public share — can be tricked into
// serving script on our origin. Anything else falls back to an attachment.
func serveContent(c *gin.Context, name string, modTime time.Time, content io.ReadSeeker, inline bool) {
	if ct, ok := safeInlineType(name); inline && ok {
		c.Header("Content-Disposition", fmt.Sprintf("inline; filename*=UTF-8''%s", url.PathEscape(name)))
		c.Header("Content-Type", ct)
		c.Header("X-Content-Type-Options", "nosniff")
	} else {
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(name)))
		c.Header("Content-Type", "application/octet-stream")
	}
	http.ServeContent(c.Writer, c.Request, name, modTime, content)
}
