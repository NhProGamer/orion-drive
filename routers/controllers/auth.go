package controllers

import (
	"context"
	"errors"
	"net/http"
	"strings"
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
		ctl.dep.Logger.Warn("OIDC exchange failed", "error", err)
		respond(c, serializer.Err(serializer.CodeUnauthorized, "authentication failed"))
		return
	}
	user, err := ctl.upsertUser(c.Request.Context(), claims)
	if err != nil {
		ctl.dep.Logger.Error("user provisioning failed", "error", err)
		respond(c, serializer.Err(serializer.CodeInternal, "internal server error"))
		return
	}
	ctl.issueSession(c, user)
	// Keep the raw ID token so logout can end the SSO session (id_token_hint).
	secure := isSecureRequest(c)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(constants.IDTokenCookieName, claims.IDToken, int(sessionTTL.Seconds()), "/", "", secure, true)
	c.Redirect(http.StatusFound, "/")
}

// AuthConfig reports which login methods are available (used by the login page).
func (ctl *Controller) AuthConfig(c *gin.Context) {
	respond(c, serializer.OK(gin.H{
		"oidc": ctl.dep.Auth.Enabled(),
		"dev":  ctl.dep.Config.System.Mode == "debug",
	}))
}

// Logout clears the session cookie and, when the provider supports it, returns
// an RP-initiated logout URL so the browser can also end the SSO session.
func (ctl *Controller) Logout(c *gin.Context) {
	idToken, _ := c.Cookie(constants.IDTokenCookieName)
	ctl.clearSession(c)
	logoutURL := ctl.dep.Auth.LogoutURL(idToken, requestOrigin(c)+"/")
	respond(c, serializer.OK(gin.H{"logout_url": logoutURL}))
}

// isSecureRequest reports whether the request reached the user over HTTPS,
// honouring a TLS-terminating reverse proxy's X-Forwarded-Proto. Used to set the
// Secure cookie flag so session cookies aren't emitted without it on an HTTPS
// site fronted by a proxy (where c.Request.TLS is nil).
func isSecureRequest(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

// requestOrigin reconstructs the browser-facing origin (scheme://host) of the
// current request, honouring a reverse proxy's forwarded scheme.
func requestOrigin(c *gin.Context) string {
	scheme := "http"
	if isSecureRequest(c) {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

// Me returns the authenticated user, or a 401.
func (ctl *Controller) Me(c *gin.Context) {
	u := ctl.user(c)
	if u == nil {
		respond(c, serializer.Err(serializer.CodeUnauthorized, "not authenticated"))
		return
	}
	canShare := true
	if u.Group != nil {
		canShare = u.Group.CanShare()
	}
	respond(c, serializer.OK(gin.H{
		"id":           u.ID,
		"email":        u.Email,
		"nick":         u.DisplayName(),
		"avatar":       u.Avatar,
		"storage_used": u.StorageUsed,
		"oidc_enabled": ctl.dep.Auth.Enabled(),
		"can_share":    canShare,
		"wopi":         ctl.dep.Config.WOPI.Enabled(),
		"boards":       ctl.dep.Boards != nil,
		"live_docs":    ctl.dep.Docs != nil,
		"admin":        middleware.IsAdmin(u, ctl.dep.Config.System.AdminEmailSet(), ctl.dep.Config.System.AdminGroupSet()),
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
	// SSO group mapping overrides the default group for new users.
	if g, ok := repo.Group.FindBySSOGroups(ctx, claims.Groups); ok {
		groupID = g.ID
	}
	u := &model.User{
		Email:     claims.Email,
		Subject:   claims.Subject,
		Nick:      claims.Name,
		Avatar:    claims.Picture,
		Status:    model.UserStatusActive,
		GroupID:   groupID,
		SSOGroups: strings.Join(claims.Groups, ","),
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
	// Persist the user's SSO groups (used for admin resolution).
	if joined := strings.Join(claims.Groups, ","); u.SSOGroups != joined {
		u.SSOGroups, changed = joined, true
	}
	// Re-sync the group from SSO on each login (SSO is the source of truth when a
	// mapping matches); leave the group unchanged when nothing maps.
	if g, ok := ctl.dep.Repo.Group.FindBySSOGroups(ctx, claims.Groups); ok && u.GroupID != g.ID {
		u.GroupID, u.Group, changed = g.ID, nil, true
	}
	if changed {
		// Detach the preloaded association so GORM writes group_id from the field.
		u.Group = nil
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
	secure := isSecureRequest(c)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(constants.SessionCookieName, token, int(sessionTTL.Seconds()), "/", "", secure, true)
	middleware.SetUser(c, u)
}

func (ctl *Controller) clearSession(c *gin.Context) {
	c.SetCookie(constants.SessionCookieName, "", -1, "/", "", false, true)
	c.SetCookie(constants.IDTokenCookieName, "", -1, "/", "", false, true)
}
