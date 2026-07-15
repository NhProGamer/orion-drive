// Package middleware holds the Gin middleware used across OrionDrive.
package middleware

import (
	"strings"

	"github.com/NhProGamer/orion-drive/application/constants"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/auth"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/gin-gonic/gin"
)

const userCtxKey = "current_user"

// CurrentUser resolves the session cookie to a user and stores it in the
// request context. It never aborts — anonymous requests simply carry no user.
func CurrentUser(signer *auth.Signer, repo *repository.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(constants.SessionCookieName)
		if err != nil || cookie == "" {
			c.Next()
			return
		}
		uid, err := signer.Verify(cookie)
		if err != nil {
			c.Next()
			return
		}
		if u, err := repo.User.GetByID(c.Request.Context(), uid); err == nil {
			c.Set(userCtxKey, u)
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
