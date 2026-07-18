package controllers

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/NhProGamer/orion-drive/pkg/wopi"
	"github.com/gin-gonic/gin"
)

// legacyOfficeExts is the fixed format list used only when the document server
// exposes no WOPI discovery but a legacy EditURLTemplate is configured.
var legacyOfficeExts = []string{
	"docx", "doc", "xlsx", "xls", "pptx", "ppt", "odt", "ods", "odp", "csv", "txt", "rtf",
}

func isLegacyOfficeExt(ext string) bool {
	return slices.Contains(legacyOfficeExts, ext)
}

// wopiTokenTTL is how long a WOPI access token stays valid.
const wopiTokenTTL = 10 * time.Hour

// wopiFile verifies the access_token, checks it is bound to :id, and loads the
// user and file. It writes a 401 and returns false on failure.
func (ctl *Controller) wopiFile(c *gin.Context) (userID, fileID uint, canWrite, ok bool) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return 0, 0, false, false
	}
	tf, tu, tw, err := ctl.dep.WOPI.Verify(c.Query("access_token"))
	if err != nil || tf != id {
		c.AbortWithStatus(http.StatusUnauthorized)
		return 0, 0, false, false
	}
	// Proof-key check (opt-in): prove the call really came from the doc server.
	if !ctl.wopiProofOK(c) {
		c.AbortWithStatus(http.StatusInternalServerError)
		return 0, 0, false, false
	}
	return tu, id, tw, true
}

// wopiProofOK verifies the WOPI proof-key signature when enabled. It fails
// closed only when verification is configured and possible: with no keys yet
// discovered it lets the request through (best-effort), but with keys present it
// requires a valid X-WOPI-Proof.
func (ctl *Controller) wopiProofOK(c *gin.Context) bool {
	if !ctl.dep.Config.WOPI.VerifyProof {
		return true
	}
	keys := ctl.dep.WOPIDisc.ProofKeys(c.Request.Context())
	if keys == nil {
		return true
	}
	proof := c.GetHeader("X-WOPI-Proof")
	tsStr := c.GetHeader("X-WOPI-TimeStamp")
	if proof == "" || tsStr == "" {
		return false
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return false
	}
	full := requestOrigin(c) + c.Request.URL.RequestURI()
	return keys.Verify(c.Query("access_token"), full, ts, proof, c.GetHeader("X-WOPI-ProofOld"))
}

// WopiCheckFileInfo returns file metadata to the Office editor (WOPI GET).
func (ctl *Controller) WopiCheckFileInfo(c *gin.Context) {
	uid, id, canWrite, ok := ctl.wopiFile(c)
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
		"UserCanWrite":     canWrite,
		"SupportsUpdate":   canWrite,
		"SupportsLocks":    false,
	})
}

// WopiGetFile streams the file's content to the editor (WOPI GET contents).
func (ctl *Controller) WopiGetFile(c *gin.Context) {
	uid, id, _, ok := ctl.wopiFile(c)
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
	uid, id, canWrite, ok := ctl.wopiFile(c)
	if !ok {
		return
	}
	if !canWrite {
		c.AbortWithStatus(http.StatusForbidden)
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

// OfficeFormats reports which extensions the document server can edit or view,
// so the UI offers the right action per file. Empty when Office is not set up.
func (ctl *Controller) OfficeFormats(c *gin.Context) {
	cfg := ctl.dep.Config.WOPI
	if !cfg.Enabled() {
		respond(c, serializer.OK(gin.H{"enabled": false, "edit": []string{}, "view": []string{}}))
		return
	}
	ctx := c.Request.Context()
	edit := ctl.dep.WOPIDisc.EditExts(ctx)
	view := ctl.dep.WOPIDisc.ViewExts(ctx)
	if len(edit) == 0 && len(view) == 0 && cfg.EditURLTemplate != "" {
		// Discovery unavailable but a legacy template is configured.
		edit, view = legacyOfficeExts, legacyOfficeExts
	}
	respond(c, serializer.OK(gin.H{"enabled": true, "edit": edit, "view": view}))
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
	// The owner always edits their own file.
	ctl.serveOfficeLauncher(c, id, u.ID, f.Name, true)
}

// OfficeLaunchShare opens a shared Office file in the editor for an anonymous
// visitor. Read shares open view-only; write shares open editable; deposit
// shares (blind) cannot open files at all. The file is resolved within the
// shared subtree and the WOPI token is minted against the share owner.
func (ctl *Controller) OfficeLaunchShare(c *gin.Context) {
	if !ctl.dep.Config.WOPI.Enabled() {
		c.String(http.StatusBadRequest, "online Office editing is not configured")
		return
	}
	fileID, ownerID, canWrite, name, err := ctl.dep.Shares.OfficeTarget(
		c.Request.Context(), c.Param("token"), c.Query("path"), c.Query("password"))
	if err != nil {
		c.String(http.StatusForbidden, "cannot open this document")
		return
	}
	ctl.serveOfficeLauncher(c, fileID, ownerID, name, canWrite)
}

// serveOfficeLauncher resolves the editor URL for the file's format from the
// document server's WOPI discovery, mints a token bound to (fileID, uid, write)
// and writes the auto-submitting launch page. The write grant is downgraded to
// view when the format has no editable action, and the request is rejected when
// the format is not supported at all.
func (ctl *Controller) serveOfficeLauncher(c *gin.Context, fileID, uid uint, name string, canWrite bool) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	base := strings.TrimRight(ctl.dep.Config.System.SiteURL, "/")
	if base == "" {
		base = requestOrigin(c)
	}
	wopiSrc := fmt.Sprintf("%s/wopi/files/%d", base, fileID)

	var action string
	editable := false
	if urlsrc, ed, ok := ctl.dep.WOPIDisc.Action(c.Request.Context(), ext, canWrite); ok {
		action = wopi.BuildActionURL(urlsrc, wopiSrc)
		editable = ed
	} else if tmpl := ctl.dep.Config.WOPI.EditURLTemplate; tmpl != "" && isLegacyOfficeExt(ext) {
		// Legacy fallback when discovery is unavailable or lacks this extension.
		action = strings.ReplaceAll(tmpl, "{type}", officeType(name))
		action = strings.ReplaceAll(action, "{src}", url.QueryEscape(wopiSrc))
		editable = canWrite
	} else {
		c.String(http.StatusBadRequest, "this file type cannot be opened in the online editor")
		return
	}

	token, err := ctl.dep.WOPI.Sign(fileID, uid, editable, wopiTokenTTL)
	if err != nil {
		c.String(http.StatusInternalServerError, "cannot start the editor")
		return
	}
	ttlMs := time.Now().Add(wopiTokenTTL).UnixMilli()

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, officeLauncherHTML(name, action, token, ttlMs))
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
