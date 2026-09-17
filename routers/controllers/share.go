package controllers

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/application/constants"
	"github.com/NhProGamer/orion-drive/application/statics"
	"github.com/NhProGamer/orion-drive/pkg/archive"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/NhProGamer/orion-drive/service/share"
	"github.com/gin-gonic/gin"
)

type createShareReq struct {
	FileID       uint   `json:"file_id"`
	Permission   string `json:"permission"`
	Password     string `json:"password"`
	ExpiresDays  int    `json:"expires_days"`
	MaxDownloads int    `json:"max_downloads"`
}

// CreateShare creates a share link for one of the user's files.
func (ctl *Controller) CreateShare(c *gin.Context) {
	var req createShareReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	s, err := ctl.dep.Shares.Create(c.Request.Context(), ctl.user(c), share.CreateOptions{
		FileID:       req.FileID,
		Permission:   req.Permission,
		Password:     req.Password,
		ExpiresIn:    time.Duration(req.ExpiresDays) * 24 * time.Hour,
		MaxDownloads: req.MaxDownloads,
	})
	if err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{
		"token": s.Token,
		"url":   ctl.shareURL(c, s.Token),
	}))
}

// ListShares returns the user's shares.
func (ctl *Controller) ListShares(c *gin.Context) {
	shares, err := ctl.dep.Shares.List(c.Request.Context(), ctl.user(c))
	if err != nil {
		failShare(c, err)
		return
	}
	ctx := c.Request.Context()
	uid := ctl.user(c).ID
	out := make([]gin.H, 0, len(shares))
	for i := range shares {
		s := &shares[i]
		// Enrich with the shared file's name/type (best-effort; a purged file
		// leaves the share pointing at nothing).
		name, isDir := "(supprimé)", false
		if f, err := ctl.dep.Repo.File.GetByIDUnscoped(ctx, uid, s.FileID); err == nil {
			name, isDir = f.Name, f.IsFolder()
		}
		out = append(out, gin.H{
			"token":            s.Token,
			"file_id":          s.FileID,
			"name":             name,
			"is_dir":           isDir,
			"permission":       s.Perm(),
			"url":              ctl.shareURL(c, s.Token),
			"has_password":     s.HasPassword(),
			"expired":          s.Expired(),
			"exhausted":        s.Exhausted(),
			"expires":          s.Expires,
			"remain_downloads": s.RemainDownloads,
			"views":            s.Views,
			"downloads":        s.Downloads,
			"created_at":       s.CreatedAt,
		})
	}
	respond(c, serializer.OK(out))
}

type updateShareReq struct {
	Permission   *string `json:"permission"`    // nil=keep, "read|write|deposit"=set
	Password     *string `json:"password"`      // nil=keep, ""=remove, "x"=set
	ExpiresDays  *int    `json:"expires_days"`  // nil=keep, <=0=never
	MaxDownloads *int    `json:"max_downloads"` // nil=keep, <=0=unlimited
}

// UpdateShare changes an existing share's settings (password/expiry/limit).
func (ctl *Controller) UpdateShare(c *gin.Context) {
	var req updateShareReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	s, err := ctl.dep.Shares.Update(c.Request.Context(), ctl.user(c), c.Param("token"), share.UpdateOptions{
		Permission:   req.Permission,
		Password:     req.Password,
		ExpiresDays:  req.ExpiresDays,
		MaxDownloads: req.MaxDownloads,
	})
	if err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{"token": s.Token, "url": ctl.shareURL(c, s.Token)}))
}

// DeleteShare removes one of the user's shares.
func (ctl *Controller) DeleteShare(c *gin.Context) {
	if err := ctl.dep.Shares.Delete(c.Request.Context(), ctl.user(c), c.Param("token")); err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// ShareView returns public metadata for a share (no auth).
func (ctl *Controller) ShareView(c *gin.Context) {
	view, err := ctl.dep.Shares.View(c.Request.Context(), c.Param("token"), c.Query("password"))
	if err != nil {
		failShare(c, err)
		return
	}
	// Advertise online Office editing when the server has it configured, so the
	// public page can offer an "open in editor" action for Office documents.
	view.Wopi = ctl.dep.Config.WOPI.Enabled()
	view.Boards = ctl.dep.Boards != nil
	view.LiveDocs = ctl.dep.Docs != nil
	respond(c, serializer.OK(view))
}

// ShareList returns the contents of a shared folder (no auth).
func (ctl *Controller) ShareList(c *gin.Context) {
	name, entries, err := ctl.dep.Shares.ListDir(c.Request.Context(), c.Param("token"), c.Query("path"), c.Query("password"))
	if err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{"name": name, "entries": entries}))
}

// ShareDownload delivers a shared file (no auth), enforcing password/expiry/limit.
// For folder shares, the file within is selected by the `path` query param.
// With `check=1` it only validates the share (204) without streaming or counting
// a download, so the public page can surface errors before navigating.
func (ctl *Controller) ShareDownload(c *gin.Context) {
	password := c.Query("password")
	if c.Query("check") != "" {
		if err := ctl.dep.Shares.Check(c.Request.Context(), c.Param("token"), c.Query("path"), password); err != nil {
			failShare(c, err)
			return
		}
		c.Status(http.StatusNoContent)
		return
	}
	// Inline preview (in-browser viewing) does not count as a download; the
	// explicit download button hits this endpoint without ?inline and is metered.
	inline := c.Query("inline") != ""
	var (
		target *filemanager.DownloadTarget
		err    error
	)
	if inline {
		target, err = ctl.dep.Shares.Inline(c.Request.Context(), c.Param("token"), c.Query("path"), password)
	} else {
		target, err = ctl.dep.Shares.Download(c.Request.Context(), c.Param("token"), c.Query("path"), password)
	}
	if err != nil {
		failShare(c, err)
		return
	}
	if target.URL != "" {
		c.Redirect(http.StatusFound, target.URL)
		return
	}
	defer target.Stream.Close()
	serveContent(c, target.File.Name, target.File.UpdatedAt, target.Stream, inline)
}

// ShareArchive streams an archive of a shared folder (or subfolder), no auth.
// `format` picks what to produce; the default is the configured one.
func (ctl *Controller) ShareArchive(c *gin.Context) {
	owner, target, err := ctl.dep.Shares.ArchiveTarget(c.Request.Context(), c.Param("token"), c.Query("path"), c.Query("password"))
	if err != nil {
		failShare(c, err)
		return
	}
	format, err := ctl.dep.Files.ArchiveFormat(c.Query("format"))
	if err != nil {
		fail(c, err)
		return
	}
	// Same reason as the authenticated download: an oversized selection has to
	// be refused before the response starts, not abandoned mid-body.
	if _, err := ctl.dep.Files.PlanArchive(c.Request.Context(), owner, []uint{target.ID}); err != nil {
		fail(c, err)
		return
	}
	filename := url.PathEscape(target.Name + archive.Extension(format))
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", filename))
	c.Header("Content-Type", "application/octet-stream")
	if err := ctl.dep.Files.WriteArchive(c.Request.Context(), owner, []uint{target.ID}, c.Writer, format, nil); err != nil {
		_ = c.Error(err)
	}
}

// --- Public write endpoints (write/deposit shares, no auth) ------------------

type shareFolderReq struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// ShareCreateFolder creates a folder inside a writable shared folder.
func (ctl *Controller) ShareCreateFolder(c *gin.Context) {
	var req shareFolderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	if err := ctl.dep.Shares.CreateFolder(c.Request.Context(), c.Param("token"), req.Path, req.Name, req.Password); err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

type shareUploadReq struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Contributor string `json:"contributor"`
	Password    string `json:"password"`
}

// ShareInitUpload starts an anonymous resumable upload into a writable share.
func (ctl *Controller) ShareInitUpload(c *gin.Context) {
	var req shareUploadReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	s, err := ctl.dep.Shares.InitUpload(c.Request.Context(), c.Param("token"), req.Path, req.Name, req.Size, req.Contributor, req.Password)
	if err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{
		"session_id": s.ID,
		"chunk_size": s.ChunkSize,
		"num_chunks": s.NumChunks(),
	}))
}

// SharePutChunk receives one chunk of an anonymous share upload.
func (ctl *Controller) SharePutChunk(c *gin.Context) {
	index, err := strconv.Atoi(c.GetHeader("X-Chunk-Index"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "missing X-Chunk-Index"))
		return
	}
	if err := ctl.dep.Shares.PutChunk(c.Request.Context(), c.Param("token"), c.Param("sid"), index, c.Request.Body); err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// ShareCompleteUpload finalizes an anonymous share upload.
func (ctl *Controller) ShareCompleteUpload(c *gin.Context) {
	if err := ctl.dep.Shares.CompleteUpload(c.Request.Context(), c.Param("token"), c.Param("sid")); err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// ShareCancelUpload aborts an anonymous share upload.
func (ctl *Controller) ShareCancelUpload(c *gin.Context) {
	if err := ctl.dep.Shares.CancelUpload(c.Request.Context(), c.Param("token"), c.Param("sid")); err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

type shareRenameReq struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// ShareRename renames an item inside a writable share.
func (ctl *Controller) ShareRename(c *gin.Context) {
	var req shareRenameReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	if err := ctl.dep.Shares.Rename(c.Request.Context(), c.Param("token"), req.Path, req.Name, req.Password); err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

type shareMoveReq struct {
	Path     string `json:"path"`
	Dest     string `json:"dest"`
	Password string `json:"password"`
}

// ShareMove relocates an item within a writable share.
func (ctl *Controller) ShareMove(c *gin.Context) {
	var req shareMoveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	if err := ctl.dep.Shares.Move(c.Request.Context(), c.Param("token"), req.Path, req.Dest, req.Password); err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

type shareDeleteReq struct {
	Path     string `json:"path"`
	Password string `json:"password"`
}

// ShareDelete moves an item in a writable share to the owner's recycle bin.
func (ctl *Controller) ShareDelete(c *gin.Context) {
	var req shareDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	if err := ctl.dep.Shares.DeleteItem(c.Request.Context(), c.Param("token"), req.Path, req.Password); err != nil {
		failShare(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// siteBase returns the public origin (scheme+host, no trailing slash) used to
// build absolute share/preview URLs, from the configured SiteURL or the request
// host as a fallback.
func (ctl *Controller) siteBase(c *gin.Context) string {
	base := ctl.dep.Config.System.SiteURL
	if base == "" {
		base = "http://" + c.Request.Host
	}
	return strings.TrimRight(base, "/")
}

// shareURL builds the public share page URL from the configured site URL.
func (ctl *Controller) shareURL(c *gin.Context, token string) string {
	return ctl.siteBase(c) + "/s/" + token
}

// ShareThumbnail serves a JPEG preview of a shared file (no auth), used as the
// OpenGraph image when a share link is unfurled. Refused for protected/expired/
// blind shares and non-thumbnailable files (404). Not counted as a download.
func (ctl *Controller) ShareThumbnail(c *gin.Context) {
	data, err := ctl.dep.Shares.Thumbnail(c.Request.Context(), c.Param("token"), c.Query("path"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "image/jpeg", data)
}

// SharePreview serves the SPA shell for /s/:token with OpenGraph/Twitter-card
// meta tags injected, so pasting a share link into a chat or social app unfurls
// with the file's name, owner and (when previewable) a thumbnail. The browser
// still boots the SPA normally; crawlers (which don't run JS) read the tags.
func (ctl *Controller) SharePreview(c *gin.Context) {
	page := statics.Index()
	if tags := ctl.shareOGTags(c, c.Param("token")); tags != "" {
		page = injectHead(page, tags)
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", page)
}

// shareOGTags builds the OpenGraph/Twitter meta tags for a share, or "" when the
// share is unknown, password-protected or expired (leaving the generic app card,
// so no metadata leaks). Uses Meta (not View) so a crawl does not count a view.
func (ctl *Controller) shareOGTags(c *gin.Context, token string) string {
	view, err := ctl.dep.Shares.Meta(c.Request.Context(), token)
	if err != nil || view.Name == "" {
		return ""
	}
	base := ctl.siteBase(c)
	title := view.Name
	desc := humanBytes(view.Size)
	if view.IsDir {
		desc = "Dossier partagé"
	}
	if view.Owner != "" {
		desc += " · " + view.Owner
	}

	var b strings.Builder
	prop := func(property, content string) {
		fmt.Fprintf(&b, `<meta property="%s" content="%s">`, property, html.EscapeString(content))
	}
	named := func(name, content string) {
		fmt.Fprintf(&b, `<meta name="%s" content="%s">`, name, html.EscapeString(content))
	}
	prop("og:type", "website")
	prop("og:site_name", "OrionDrive")
	prop("og:title", title)
	prop("og:description", desc)
	prop("og:url", base+"/s/"+token)
	card := "summary"
	if view.Previewable {
		img := base + constants.APIPrefix + "/share/" + url.PathEscape(token) + "/thumb"
		prop("og:image", img)
		named("twitter:image", img)
		card = "summary_large_image"
	}
	named("twitter:card", card)
	named("twitter:title", title)
	named("twitter:description", desc)
	return b.String()
}

// injectHead inserts snippet immediately before the first </head> in page. If no
// head close tag is present the page is returned unchanged.
func injectHead(page []byte, snippet string) []byte {
	i := bytes.Index(page, []byte("</head>"))
	if i < 0 {
		return page
	}
	out := make([]byte, 0, len(page)+len(snippet))
	out = append(out, page[:i]...)
	out = append(out, snippet...)
	out = append(out, page[i:]...)
	return out
}

// humanBytes formats a byte count with a binary (1024) unit in French octets.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d o", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %co", float64(n)/float64(div), "kMGTPE"[exp])
}

// failShare maps share-domain errors to response codes.
func failShare(c *gin.Context, err error) {
	switch {
	case errors.Is(err, share.ErrNotFound):
		respond(c, serializer.Err(serializer.CodeNotFound, "share not found"))
	case errors.Is(err, share.ErrPasswordRequired), errors.Is(err, share.ErrWrongPassword):
		respond(c, serializer.Err(serializer.CodeUnauthorized, err.Error()))
	case errors.Is(err, share.ErrExpired), errors.Is(err, share.ErrExhausted), errors.Is(err, share.ErrNotAllowed), errors.Is(err, share.ErrForbidden):
		respond(c, serializer.Err(serializer.CodeForbidden, err.Error()))
	case errors.Is(err, share.ErrNotAFile), errors.Is(err, share.ErrNotAFolder), errors.Is(err, share.ErrBadPermission):
		respond(c, serializer.Err(serializer.CodeBadRequest, err.Error()))
	default:
		// Write operations surface filemanager errors (conflict, quota, lock,
		// invalid name); let the shared mapper handle those.
		fail(c, err)
	}
}
