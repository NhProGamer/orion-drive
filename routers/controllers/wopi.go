package controllers

import (
	"crypto/rand"
	"encoding/base64"
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
func (ctl *Controller) wopiFile(c *gin.Context) (userID, fileID uint, canWrite bool, editorID, editorName string, ok bool) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return 0, 0, false, "", "", false
	}
	tf, tu, tw, ts, ei, en, err := ctl.dep.WOPI.Verify(c.Query("access_token"))
	if err != nil || tf != id {
		c.AbortWithStatus(http.StatusUnauthorized)
		return 0, 0, false, "", "", false
	}
	// For a share-originated session, revalidate the share on every call so that
	// deleting/expiring/downgrading it takes effect within the token lifetime
	// (the token alone is otherwise a self-contained 10h capability). A write
	// grant is downgraded to read-only if the share lost write permission.
	if ts != "" {
		cw, valid := ctl.dep.Shares.WOPIStillValid(c.Request.Context(), ts)
		if !valid {
			c.AbortWithStatus(http.StatusUnauthorized)
			return 0, 0, false, "", "", false
		}
		tw = tw && cw
	}
	// Proof-key check (opt-in): prove the call really came from the doc server.
	if !ctl.wopiProofOK(c) {
		c.AbortWithStatus(http.StatusInternalServerError)
		return 0, 0, false, "", "", false
	}
	return tu, id, tw, ei, en, true
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
	uid, id, canWrite, editorID, editorName, ok := ctl.wopiFile(c)
	if !ok {
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
		"UserId":           editorID,
		"UserFriendlyName": editorName,
		"Version":          version,
		"UserCanWrite":     canWrite,
		"SupportsUpdate":   canWrite,
		"SupportsLocks":    true,
		"SupportsGetLock":  true,
	})
}

// wopiLockTTL is how long a WOPI lock survives without a refresh (the value the
// spec mandates editors refresh within).
const wopiLockTTL = 30 * time.Minute

func wopiLockKey(id uint) string { return "wopi:lock:" + strconv.FormatUint(uint64(id), 10) }

// wopiGetLock returns the current lock string for a file ("" if unlocked).
func (ctl *Controller) wopiGetLock(id uint) string {
	if b, ok := ctl.dep.Cache.Get(wopiLockKey(id)); ok {
		return string(b)
	}
	return ""
}

func (ctl *Controller) wopiSetLock(id uint, lock string) {
	_ = ctl.dep.Cache.Set(wopiLockKey(id), []byte(lock), wopiLockTTL)
}

func (ctl *Controller) wopiClearLock(id uint) {
	_ = ctl.dep.Cache.Delete(wopiLockKey(id))
}

// wopiLockConflict answers a lock-mismatch with 409 and the current lock, as the
// WOPI protocol requires so the editor can reconcile.
func wopiLockConflict(c *gin.Context, current string) {
	c.Header("X-WOPI-Lock", current)
	c.Status(http.StatusConflict)
}

// WopiLock handles the WOPI lock operations dispatched by the X-WOPI-Override
// header (LOCK, UNLOCK, REFRESH_LOCK, GET_LOCK) so concurrent editors don't
// silently clobber each other. Locks are held in the shared cache and expire
// after wopiLockTTL if the editor stops refreshing.
func (ctl *Controller) WopiLock(c *gin.Context) {
	_, id, canWrite, _, _, ok := ctl.wopiFile(c)
	if !ok {
		return
	}
	op := c.GetHeader("X-WOPI-Override")
	current := ctl.wopiGetLock(id)

	// GET_LOCK is read-only and always allowed.
	if op == "GET_LOCK" {
		c.Header("X-WOPI-Lock", current)
		c.Status(http.StatusOK)
		return
	}
	if !canWrite {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	lock := c.GetHeader("X-WOPI-Lock")
	switch op {
	case "LOCK":
		// Unlock-and-relock variant: the caller supplies the lock it expects to
		// replace; only proceed when it matches the one we hold.
		if old := c.GetHeader("X-WOPI-OldLock"); old != "" {
			if current != old {
				wopiLockConflict(c, current)
				return
			}
			ctl.wopiSetLock(id, lock)
			c.Status(http.StatusOK)
			return
		}
		if current == "" || current == lock {
			ctl.wopiSetLock(id, lock) // take the lock, or refresh our own
			c.Status(http.StatusOK)
			return
		}
		wopiLockConflict(c, current)
	case "REFRESH_LOCK":
		if current != "" && current == lock {
			ctl.wopiSetLock(id, lock)
			c.Status(http.StatusOK)
			return
		}
		wopiLockConflict(c, current)
	case "UNLOCK":
		if current != "" && current == lock {
			ctl.wopiClearLock(id)
			c.Status(http.StatusOK)
			return
		}
		wopiLockConflict(c, current)
	default:
		// PutRelativeFile, RenameFile, and other overrides are not supported.
		c.AbortWithStatus(http.StatusNotImplemented)
	}
}

// WopiGetFile streams the file's content to the editor (WOPI GET contents).
func (ctl *Controller) WopiGetFile(c *gin.Context) {
	uid, id, _, _, _, ok := ctl.wopiFile(c)
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
	uid, id, canWrite, _, _, ok := ctl.wopiFile(c)
	if !ok {
		return
	}
	if !canWrite {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	// Enforce the lock: reject a save that carries a different lock than the one
	// currently held, so a second editor cannot overwrite the first's session.
	// An unlocked file is still writable (first save, or an editor that does not
	// lock) — this only blocks a genuine lock clash.
	if current := ctl.wopiGetLock(id); current != "" && current != c.GetHeader("X-WOPI-Lock") {
		wopiLockConflict(c, current)
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
	// "new" are formats the server can create blank (editnew action). This needs
	// a real document server, so there is no legacy fallback for it.
	create := ctl.dep.WOPIDisc.NewExts(ctx)
	if len(edit) == 0 && len(view) == 0 && cfg.EditURLTemplate != "" {
		// Discovery unavailable but a legacy template is configured.
		edit, view = legacyOfficeExts, legacyOfficeExts
	}
	respond(c, serializer.OK(gin.H{"enabled": true, "edit": edit, "view": view, "new": create}))
}

type officeNewReq struct {
	Parent string `json:"parent"`
	Name   string `json:"name"`
}

// OfficeNew creates a blank document of a WOPI-creatable format in a folder and
// returns it, so the caller can open it in the editor. The extension must have
// an editnew action in the document server's discovery; the file is stored empty
// and the editor initialises the blank content on first save.
func (ctl *Controller) OfficeNew(c *gin.Context) {
	if !ctl.dep.Config.WOPI.Enabled() {
		respond(c, serializer.Err(serializer.CodeBadRequest, "online Office editing is not configured"))
		return
	}
	var req officeNewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(req.Name), "."))
	if _, ok := ctl.dep.WOPIDisc.NewAction(c.Request.Context(), ext); !ok {
		respond(c, serializer.Err(serializer.CodeBadRequest, "this file type cannot be created"))
		return
	}
	parentID, err := parseParentID(req.Parent)
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid parent"))
		return
	}
	u := ctl.user(c)
	f, err := ctl.dep.Files.WriteFile(c.Request.Context(), u, parentID, req.Name, strings.NewReader(""), 0)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTO(f, u.DisplayName())))
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
	// The owner always edits their own file; an empty one opens as a new document.
	// No share token: the owner's own access never needs revalidation. The editor
	// id equals the owner id (matches OwnerId in CheckFileInfo) so the document
	// server recognises them as the owner.
	ctl.serveOfficeLauncher(c, id, u.ID, f.Name, true, f.Size == 0, "", strconv.FormatUint(uint64(u.ID), 10), u.DisplayName())
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
	// Bind the token to the share so each WOPI call revalidates it (revocation).
	// Every share visitor gets a distinct guest editor identity (unique per open)
	// so co-editors show up as separate users with their own name, not all as the
	// file owner. The display name is what the visitor typed, or a generic label.
	editorName := strings.TrimSpace(c.Query("name"))
	if editorName == "" {
		editorName = "Invité"
	}
	ctl.serveOfficeLauncher(c, fileID, ownerID, name, canWrite, false, c.Param("token"), guestEditorID(), editorName)
}

// guestEditorID returns a random opaque identifier for an anonymous share
// visitor's editing session. It is distinct from any real user id (and from the
// file's OwnerId), so the document server treats concurrent share editors as
// separate, non-owner participants.
func guestEditorID() string {
	var b [9]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "g0"
	}
	return "g" + base64.RawURLEncoding.EncodeToString(b[:])
}

// serveOfficeLauncher resolves the editor URL for the file's format from the
// document server's WOPI discovery, mints a token bound to (fileID, uid, write)
// and writes the auto-submitting launch page. The write grant is downgraded to
// view when the format has no editable action, and the request is rejected when
// the format is not supported at all.
func (ctl *Controller) serveOfficeLauncher(c *gin.Context, fileID, uid uint, name string, canWrite, preferNew bool, shareToken, editorID, editorName string) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	base := strings.TrimRight(ctl.dep.Config.System.SiteURL, "/")
	if base == "" {
		base = requestOrigin(c)
	}
	wopiSrc := fmt.Sprintf("%s/wopi/files/%d", base, fileID)

	var action string
	editable := false
	ctx := c.Request.Context()
	// A freshly-created empty document opens via the editnew action so the server
	// initialises blank content; existing files use the normal edit/view action.
	if preferNew {
		if urlsrc, ok := ctl.dep.WOPIDisc.NewAction(ctx, ext); ok {
			action = wopi.BuildActionURL(urlsrc, wopiSrc)
			editable = true
		}
	}
	if action == "" {
		if urlsrc, ed, ok := ctl.dep.WOPIDisc.Action(ctx, ext, canWrite); ok {
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
	}

	token, err := ctl.dep.WOPI.Sign(fileID, uid, editable, shareToken, editorID, editorName, wopiTokenTTL)
	if err != nil {
		c.String(http.StatusInternalServerError, "cannot start the editor")
		return
	}
	ttlMs := time.Now().Add(wopiTokenTTL).UnixMilli()

	// Self-contained CSP for the launcher: it runs an inline submit script and
	// frames/posts to the document server, so it overrides any strict global
	// System.CSP that would otherwise block Office editing.
	if origin := originOf(action); origin != "" {
		c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; form-action "+origin+"; frame-src "+origin)
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, officeLauncherHTML(name, action, token, ttlMs))
}

// originOf returns the scheme://host origin of a URL, or "" if it cannot be
// parsed to an absolute URL.
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
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
