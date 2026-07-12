package controllers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/NhProGamer/orion-drive/application/constants"
	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/auth"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/gin-gonic/gin"
)

const sessionTTL = 7 * 24 * time.Hour

// OIDCLogin redirects the browser to the OIDC provider.
func (ctl *Controller) OIDCLogin(c *gin.Context) {
	url, err := ctl.dep.Auth.AuthCodeURL()
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, err.Error()))
		return
	}
	c.Redirect(http.StatusFound, url)
}

// OIDCCallback completes the login: it verifies the code, provisions the user,
// sets the session cookie and redirects to the SPA.
func (ctl *Controller) OIDCCallback(c *gin.Context) {
	claims, err := ctl.dep.Auth.Exchange(c.Request.Context(), c.Query("state"), c.Query("code"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeUnauthorized, err.Error()))
		return
	}
	user, err := ctl.upsertUser(c.Request.Context(), claims)
	if err != nil {
		respond(c, serializer.Err(serializer.CodeInternal, err.Error()))
		return
	}
	ctl.issueSession(c, user)
	c.Redirect(http.StatusFound, "/")
}

// Logout clears the session cookie.
func (ctl *Controller) Logout(c *gin.Context) {
	ctl.clearSession(c)
	respond(c, serializer.OK(nil))
}

// Me returns the authenticated user, or a 401.
func (ctl *Controller) Me(c *gin.Context) {
	u := ctl.user(c)
	if u == nil {
		respond(c, serializer.Err(serializer.CodeUnauthorized, "not authenticated"))
		return
	}
	respond(c, serializer.OK(gin.H{
		"id":           u.ID,
		"email":        u.Email,
		"nick":         u.DisplayName(),
		"avatar":       u.Avatar,
		"storage_used": u.StorageUsed,
		"oidc_enabled": ctl.dep.Auth.Enabled(),
	}))
}

// DevLogin is a development-only shortcut that logs in a fixed local account
// without an external IdP. It is registered only when Mode=debug, so it never
// exists in a production build.
func (ctl *Controller) DevLogin(c *gin.Context) {
	user, err := ctl.upsertUser(c.Request.Context(), &auth.Claims{
		Subject: "dev-subject",
		Email:   "dev@orion.local",
		Name:    "Dev User",
	})
	if err != nil {
		respond(c, serializer.Err(serializer.CodeInternal, err.Error()))
		return
	}
	ctl.issueSession(c, user)
	c.Redirect(http.StatusFound, "/")
}

// upsertUser links or creates the account for a set of OIDC claims.
func (ctl *Controller) upsertUser(ctx context.Context, claims *auth.Claims) (*model.User, error) {
	repo := ctl.dep.Repo
	if u, err := repo.User.GetBySubject(ctx, claims.Subject); err == nil {
		return ctl.syncUser(ctx, u, claims)
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	// Fall back to linking by email for accounts created before OIDC linkage.
	if claims.Email != "" {
		if u, err := repo.User.GetByEmail(ctx, claims.Email); err == nil {
			u.Subject = claims.Subject
			return ctl.syncUser(ctx, u, claims)
		}
	}
	groupID, err := repo.User.DefaultGroupID(ctx)
	if err != nil {
		return nil, err
	}
	u := &model.User{
		Email:   claims.Email,
		Subject: claims.Subject,
		Nick:    claims.Name,
		Avatar:  claims.Picture,
		Status:  model.UserStatusActive,
		GroupID: groupID,
	}
	if err := repo.User.Create(ctx, u); err != nil {
		return nil, err
	}
	return repo.User.GetByID(ctx, u.ID)
}

// syncUser refreshes mutable profile fields from the latest claims.
func (ctl *Controller) syncUser(ctx context.Context, u *model.User, claims *auth.Claims) (*model.User, error) {
	changed := false
	if claims.Name != "" && u.Nick != claims.Name {
		u.Nick, changed = claims.Name, true
	}
	if claims.Picture != "" && u.Avatar != claims.Picture {
		u.Avatar, changed = claims.Picture, true
	}
	if u.Subject == "" && claims.Subject != "" {
		u.Subject, changed = claims.Subject, true
	}
	if changed {
		if err := ctl.dep.Repo.User.Update(ctx, u); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func (ctl *Controller) issueSession(c *gin.Context, u *model.User) {
	token, err := ctl.dep.Signer.Sign(u.ID, sessionTTL)
	if err != nil {
		respond(c, serializer.Err(serializer.CodeInternal, "failed to issue session"))
		return
	}
	secure := c.Request.TLS != nil
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(constants.SessionCookieName, token, int(sessionTTL.Seconds()), "/", "", secure, true)
	middleware.SetUser(c, u)
}

func (ctl *Controller) clearSession(c *gin.Context) {
	c.SetCookie(constants.SessionCookieName, "", -1, "/", "", false, true)
}
