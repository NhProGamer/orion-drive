package controllers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// wopiTokenTTL is how long a WOPI access token stays valid.
const wopiTokenTTL = 10 * time.Hour

// wopiFile verifies the access_token, checks it is bound to :id, and loads the
// user and file. It writes a 401 and returns false on failure.
func (ctl *Controller) wopiFile(c *gin.Context) (userID, fileID uint, ok bool) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return 0, 0, false
	}
	tf, tu, err := ctl.dep.WOPI.Verify(c.Query("access_token"))
	if err != nil || tf != id {
		c.AbortWithStatus(http.StatusUnauthorized)
		return 0, 0, false
	}
	return tu, id, true
}

// WopiCheckFileInfo returns file metadata to the Office editor (WOPI GET).
func (ctl *Controller) WopiCheckFileInfo(c *gin.Context) {
	uid, id, ok := ctl.wopiFile(c)
	if !ok {
		return
	}
	u, err := ctl.dep.Repo.User.GetByID(c.Request.Context(), uid)
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	f, err := ctl.dep.Repo.File.GetByID(c.Request.Context(), uid, id)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	version := "0"
	if f.PrimaryEntityID != nil {
		version = fmt.Sprintf("%d", *f.PrimaryEntityID)
	}
	c.JSON(http.StatusOK, gin.H{
		"BaseFileName":     f.Name,
		"Size":             f.Size,
		"OwnerId":          fmt.Sprintf("%d", f.OwnerID),
		"UserId":           fmt.Sprintf("%d", uid),
		"UserFriendlyName": u.DisplayName(),
		"Version":          version,
		"UserCanWrite":     true,
		"SupportsUpdate":   true,
		"SupportsLocks":    false,
	})
}

// WopiGetFile streams the file's content to the editor (WOPI GET contents).
func (ctl *Controller) WopiGetFile(c *gin.Context) {
	uid, id, ok := ctl.wopiFile(c)
	if !ok {
		return
	}
	u, err := ctl.dep.Repo.User.GetByID(c.Request.Context(), uid)
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	rc, f, err := ctl.dep.Files.OpenFileContent(c.Request.Context(), u, id)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	defer rc.Close()
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-WOPI-ItemVersion", fmt.Sprintf("%d", f.ID))
	_, _ = io.Copy(c.Writer, rc)
}

// WopiPutFile saves editor changes as a new version (WOPI POST contents).
func (ctl *Controller) WopiPutFile(c *gin.Context) {
	uid, id, ok := ctl.wopiFile(c)
	if !ok {
		return
	}
	u, err := ctl.dep.Repo.User.GetByID(c.Request.Context(), uid)
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBlobSize))
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	if _, err := ctl.dep.Files.SaveVersion(c.Request.Context(), u, id, body); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusOK)
}

// OfficeLaunch returns the URL of the Office editor for a file (authenticated).
func (ctl *Controller) OfficeLaunch(c *gin.Context) {
	if !ctl.dep.Config.WOPI.Enabled() {
		respond(c, serializer.Err(serializer.CodeBadRequest, "online Office editing is not configured"))
		return
	}
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	u := ctl.user(c)
	if rc, _, err := ctl.dep.Files.OpenFileContent(c.Request.Context(), u, id); err != nil {
		fail(c, err)
		return
	} else {
		_ = rc.Close()
	}
	token, err := ctl.dep.WOPI.Sign(id, u.ID, wopiTokenTTL)
	if err != nil {
		fail(c, err)
		return
	}
	base := strings.TrimRight(ctl.dep.Config.System.SiteURL, "/")
	wopiSrc := fmt.Sprintf("%s/wopi/files/%d", base, id)
	editor := strings.ReplaceAll(ctl.dep.Config.WOPI.EditURLTemplate, "{src}", url.QueryEscape(wopiSrc))
	sep := "?"
	if strings.Contains(editor, "?") {
		sep = "&"
	}
	respond(c, serializer.OK(gin.H{
		"url":      editor + sep + "access_token=" + url.QueryEscape(token),
		"wopi_src": wopiSrc,
	}))
}
