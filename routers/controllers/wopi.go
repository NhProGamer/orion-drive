package controllers

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

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

// officeType maps a file extension to the OnlyOffice WOPI editor type segment
// (word | cell | slide) used in the action URL.
func officeType(name string) string {
	switch strings.ToLower(strings.TrimPrefix(path.Ext(name), ".")) {
	case "xlsx", "xls", "ods", "csv", "xlsm", "xlt":
		return "cell"
	case "pptx", "ppt", "odp", "pps":
		return "slide"
	default: // docx, doc, odt, rtf, txt, ...
		return "word"
	}
}

// OfficeLaunch serves an HTML page that opens the file in the online Office
// editor. A WOPI editor is launched by POSTing a form (access_token in the body)
// to the editor's action URL and hosting it in an iframe — a plain browser GET to
// that URL returns 404. This page performs that auto-submitting POST.
func (ctl *Controller) OfficeLaunch(c *gin.Context) {
	if !ctl.dep.Config.WOPI.Enabled() {
		c.String(http.StatusBadRequest, "online Office editing is not configured")
		return
	}
	id, err := parseUint(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "invalid id")
		return
	}
	u := ctl.user(c)
	f, err := ctl.dep.Repo.File.GetByID(c.Request.Context(), u.ID, id)
	if err != nil || f.IsFolder() {
		c.String(http.StatusNotFound, "file not found")
		return
	}
	token, err := ctl.dep.WOPI.Sign(id, u.ID, wopiTokenTTL)
	if err != nil {
		c.String(http.StatusInternalServerError, "cannot start the editor")
		return
	}

	base := strings.TrimRight(ctl.dep.Config.System.SiteURL, "/")
	wopiSrc := fmt.Sprintf("%s/wopi/files/%d", base, id)
	// {type} (if present) → word/cell/slide by file kind; {src} → encoded WOPISrc.
	action := strings.ReplaceAll(ctl.dep.Config.WOPI.EditURLTemplate, "{type}", officeType(f.Name))
	action = strings.ReplaceAll(action, "{src}", url.QueryEscape(wopiSrc))
	ttlMs := time.Now().Add(wopiTokenTTL).UnixMilli()

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, officeLauncherHTML(f.Name, action, token, ttlMs))
}

// officeLauncherHTML builds the auto-submitting WOPI launch page.
func officeLauncherHTML(name, action, token string, ttlMs int64) string {
	e := html.EscapeString
	return `<!doctype html><html lang="fr"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>` + e(name) + `</title>` +
		`<style>html,body{margin:0;height:100%;background:#1a1a1a}iframe{border:0;width:100%;height:100%;display:block}</style>` +
		`</head><body>` +
		`<form id="wopi" method="post" target="office_frame" action="` + e(action) + `">` +
		`<input type="hidden" name="access_token" value="` + e(token) + `">` +
		`<input type="hidden" name="access_token_ttl" value="` + fmt.Sprintf("%d", ttlMs) + `">` +
		`</form>` +
		`<iframe name="office_frame" allowfullscreen allow="autoplay; camera; microphone; display-capture"></iframe>` +
		`<script>document.getElementById('wopi').submit()</script>` +
		`</body></html>`
}
