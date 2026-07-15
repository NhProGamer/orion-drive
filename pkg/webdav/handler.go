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

const userKey ctxKey = 0

func withUser(ctx context.Context, u *model.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

func userFromCtx(ctx context.Context) *model.User {
	u, _ := ctx.Value(userKey).(*model.User)
	return u
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
	dav := &xwebdav.Handler{
		Prefix:     Prefix,
		FileSystem: NewFS(mgr, repo),
		LockSystem: xwebdav.NewMemLS(),
	}
	return &authHandler{repo: repo, dav: dav}
}

type authHandler struct {
	repo *repository.Repository
	dav  *xwebdav.Handler
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

	ctx := withUser(r.Context(), user)
	h.dav.ServeHTTP(w, r.WithContext(ctx))
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="OrionDrive WebDAV"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
