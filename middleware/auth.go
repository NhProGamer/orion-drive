// Package middleware holds the Gin middleware used across OrionDrive.
package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/application/constants"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/auth"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/gin-gonic/gin"
)

const (
	userCtxKey       = "current_user"
	tokenReadOnlyKey = "token_read_only"
)

// CurrentUser resolves the request's identity — the session cookie, or a Bearer
// personal access token — and stores the user in the context. It never aborts:
// anonymous requests simply carry no user.
func CurrentUser(signer *auth.Signer, repo *repository.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		if u := userFromCookie(c, signer, repo); u != nil {
			c.Set(userCtxKey, u)
			c.Next()
			return
		}
		if u, readOnly := userFromToken(c, repo); u != nil {
			c.Set(userCtxKey, u)
			if readOnly {
				c.Set(tokenReadOnlyKey, true)
			}
		}
		c.Next()
	}
}

func userFromCookie(c *gin.Context, signer *auth.Signer, repo *repository.Repository) *model.User {
	cookie, err := c.Cookie(constants.SessionCookieName)
	if err != nil || cookie == "" {
		return nil
	}
	uid, err := signer.Verify(cookie)
	if err != nil {
		return nil
	}
	u, err := repo.User.GetByID(c.Request.Context(), uid)
	if err != nil {
		return nil
	}
	return u
}

// userFromToken authenticates an "Authorization: Bearer <token>" personal access
// token, returning the owning user and whether the token is read-only.
func userFromToken(c *gin.Context, repo *repository.Repository) (*model.User, bool) {
	h := c.GetHeader("Authorization")
	const p = "Bearer "
	if len(h) <= len(p) || !strings.EqualFold(h[:len(p)], p) {
		return nil, false
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(h[len(p):])))
	tok, err := repo.APIToken.GetByHash(c.Request.Context(), hex.EncodeToString(sum[:]))
	if err != nil || tok.Expired() {
		return nil, false
	}
	u, err := repo.User.GetByID(c.Request.Context(), tok.UserID)
	if err != nil {
		return nil, false
	}
	// Record use, throttled to avoid a write on every request.
	now := time.Now()
	if tok.LastUsedAt == nil || now.Sub(*tok.LastUsedAt) > time.Minute {
		_ = repo.APIToken.TouchLastUsed(c.Request.Context(), tok.ID, now)
	}
	return u, tok.ReadOnly
}

// EnforceReadOnlyToken blocks unsafe HTTP methods when the request is
// authenticated by a read-only token (safe methods pass through). Session and
// read-write token users are unaffected.
func EnforceReadOnlyToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		if ro, _ := c.Get(tokenReadOnlyKey); ro == true {
			switch c.Request.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				r := serializer.Err(serializer.CodeForbidden, "read-only token")
				c.AbortWithStatusJSON(r.HTTPStatus(), r)
				return
			}
		}
		c.Next()
	}
}

// RequireAuth aborts unauthenticated requests with 401.
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := c.Get(userCtxKey); !ok {
			r := serializer.Err(serializer.CodeUnauthorized, "authentication required")
			c.AbortWithStatusJSON(r.HTTPStatus(), r)
			return
		}
		c.Next()
	}
}

// IsAdmin reports whether a user has administrator access: their email is in the
// bootstrap allowlist, one of their SSO groups is an admin group, or their
// OrionDrive group carries the admin permission.
func IsAdmin(u *model.User, adminEmails, adminGroups map[string]bool) bool {
	if u == nil {
		return false
	}
	if adminEmails[strings.ToLower(u.Email)] {
		return true
	}
	if len(adminGroups) > 0 {
		for _, g := range u.SSOGroupList() {
			if adminGroups[strings.ToLower(g)] {
				return true
			}
		}
	}
	return u.Group != nil && u.Group.CanAdmin()
}

// RequireAdmin aborts requests from non-admin users (use after RequireAuth).
func RequireAdmin(adminEmails, adminGroups map[string]bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsAdmin(UserFrom(c), adminEmails, adminGroups) {
			r := serializer.Err(serializer.CodeForbidden, "administrator access required")
			c.AbortWithStatusJSON(r.HTTPStatus(), r)
			return
		}
		c.Next()
	}
}

// UserFrom returns the authenticated user, or nil.
func UserFrom(c *gin.Context) *model.User {
	if v, ok := c.Get(userCtxKey); ok {
		return v.(*model.User)
	}
	return nil
}

// SetUser stores a user in the context (used right after login).
func SetUser(c *gin.Context, u *model.User) { c.Set(userCtxKey, u) }
