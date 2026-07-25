package webdav

import (
	"compress/gzip"
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/repository"
	"golang.org/x/crypto/bcrypt"
	xwebdav "golang.org/x/net/webdav"
)

// Prefix is the URL path prefix WebDAV is served under.
const Prefix = "/dav"

// ctxKey is the private context key type for the authenticated user.
type ctxKey int

const (
	userKey  ctxKey = 0
	lenKey   ctxKey = 1
	mtimeKey ctxKey = 2
)

func withUser(ctx context.Context, u *model.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

func userFromCtx(ctx context.Context) *model.User {
	u, _ := ctx.Value(userKey).(*model.User)
	return u
}

// withContentLength stashes the request's declared body length so a PUT can be
// streamed straight to storage yet still reject a short/cut upload.
func withContentLength(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, lenKey, n)
}

func contentLengthFromCtx(ctx context.Context) int64 {
	if n, ok := ctx.Value(lenKey).(int64); ok {
		return n
	}
	return -1
}

// withMtime stashes a client-supplied modification time (WebDAV X-OC-Mtime) so a
// PUT can preserve the file's mtime.
func withMtime(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, mtimeKey, t)
}

func mtimeFromCtx(ctx context.Context) (time.Time, bool) {
	t, ok := ctx.Value(mtimeKey).(time.Time)
	return t, ok
}

// writeMethods are the HTTP methods that modify storage; read-only accounts are
// forbidden from using them.
var writeMethods = map[string]bool{
	http.MethodPut:    true,
	http.MethodDelete: true,
	"MKCOL":           true,
	"COPY":            true,
	"MOVE":            true,
	"PROPPATCH":       true,
	"LOCK":            true,
	"UNLOCK":          true,
}

// Handler builds the authenticated WebDAV HTTP handler mounted at /dav. Clients
// authenticate with dedicated WebDAV credentials (HTTP Basic), not the OIDC
// session, since WebDAV clients cannot perform an interactive OIDC flow.
func Handler(mgr *filemanager.Manager, repo *repository.Repository) http.Handler {
	return &authHandler{repo: repo, fs: NewFS(mgr, repo)}
}

type authHandler struct {
	repo *repository.Repository
	fs   *FS
}

func (h *authHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok {
		unauthorized(w)
		return
	}
	acct, err := h.repo.WebDAV.GetByUsername(r.Context(), username)
	if err != nil {
		unauthorized(w)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(acct.PasswordHash), []byte(password)) != nil {
		unauthorized(w)
		return
	}
	if acct.ReadOnly && writeMethods[strings.ToUpper(r.Method)] {
		http.Error(w, "read-only WebDAV account", http.StatusForbidden)
		return
	}
	user, err := h.repo.User.GetByID(r.Context(), acct.UserID)
	if err != nil {
		unauthorized(w)
		return
	}
	_ = h.repo.WebDAV.TouchLastUsed(r.Context(), acct.ID)

	// SEARCH (RFC 5323) and REPORT (RFC 6578 sync-collection) aren't handled by
	// x/net/webdav; answer them ourselves.
	switch strings.ToUpper(r.Method) {
	case "SEARCH":
		h.handleSearch(w, r.WithContext(withUser(r.Context(), user)), user)
		return
	case "REPORT":
		h.handleReport(w, r.WithContext(withUser(r.Context(), user)), user)
		return
	}

	// Build the WebDAV handler per request so the lock system is bound to this
	// user (locks are persisted in the DB and isolated per user).
	dav := &xwebdav.Handler{
		Prefix:     Prefix,
		FileSystem: h.fs,
		LockSystem: newLocks(h.repo, user.ID),
	}
	ctx := withUser(r.Context(), user)
	ctx = withContentLength(ctx, r.ContentLength)
	// Honour a client-supplied modification time (rclone / ownCloud X-OC-Mtime).
	if v := r.Header.Get("X-OC-Mtime"); v != "" {
		if secs, perr := strconv.ParseInt(v, 10, 64); perr == nil && secs > 0 {
			ctx = withMtime(ctx, time.Unix(secs, 0))
			w.Header().Set("X-OC-Mtime", "accepted")
		}
	}

	// gzip the (verbose, highly compressible) XML multistatus responses when the
	// client accepts it. Not applied to GET so file downloads keep Range support
	// and aren't needlessly recompressed.
	if xmlResponseMethod(r.Method) && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		w = &gzipResponder{ResponseWriter: w, gz: gz}
	}

	dav.ServeHTTP(w, r.WithContext(ctx))
}

// xmlResponseMethod reports whether the WebDAV method returns an XML multistatus
// body worth compressing.
func xmlResponseMethod(method string) bool {
	switch strings.ToUpper(method) {
	case "PROPFIND", "PROPPATCH":
		return true
	}
	return false
}

// gzipResponder gzips the response body. Content-Encoding/Vary are set by the
// caller before any write.
type gzipResponder struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipResponder) Write(b []byte) (int, error) { return g.gz.Write(b) }

func (g *gzipResponder) Flush() {
	_ = g.gz.Flush()
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="OrionDrive WebDAV"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
