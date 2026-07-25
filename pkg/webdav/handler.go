package webdav

import (
	"context"
	"net/http"
	"strings"

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
	userKey ctxKey = 0
	lenKey  ctxKey = 1
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

	// Build the WebDAV handler per request so the lock system is bound to this
	// user (locks are persisted in the DB and isolated per user).
	dav := &xwebdav.Handler{
		Prefix:     Prefix,
		FileSystem: h.fs,
		LockSystem: newLocks(h.repo, user.ID),
	}
	ctx := withUser(r.Context(), user)
	ctx = withContentLength(ctx, r.ContentLength)
	dav.ServeHTTP(w, r.WithContext(ctx))
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="OrionDrive WebDAV"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
