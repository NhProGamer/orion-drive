package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/NhProGamer/orion-drive/service/share"
	"github.com/gin-gonic/gin"
)

type createShareReq struct {
	FileID       uint   `json:"file_id"`
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
	view, err := ctl.dep.Shares.View(c.Request.Context(), c.Param("token"))
	if err != nil {
		failShare(c, err)
		return
	}
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
	target, err := ctl.dep.Shares.Download(c.Request.Context(), c.Param("token"), c.Query("path"), password)
	if err != nil {
		failShare(c, err)
		return
	}
	if target.URL != "" {
		c.Redirect(http.StatusFound, target.URL)
		return
	}
	defer target.Stream.Close()
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(target.File.Name)))
	c.Header("Content-Type", "application/octet-stream")
	http.ServeContent(c.Writer, c.Request, target.File.Name, target.File.UpdatedAt, target.Stream)
}

// ShareArchive streams a ZIP of a shared folder (or subfolder), no auth.
func (ctl *Controller) ShareArchive(c *gin.Context) {
	owner, target, err := ctl.dep.Shares.ArchiveTarget(c.Request.Context(), c.Param("token"), c.Query("path"), c.Query("password"))
	if err != nil {
		failShare(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s.zip", url.PathEscape(target.Name)))
	c.Header("Content-Type", "application/zip")
	if err := ctl.dep.Files.WriteArchive(c.Request.Context(), owner, []uint{target.ID}, c.Writer); err != nil {
		_ = c.Error(err)
	}
}

// shareURL builds the public share page URL from the configured site URL.
func (ctl *Controller) shareURL(c *gin.Context, token string) string {
	base := ctl.dep.Config.System.SiteURL
	if base == "" {
		base = "http://" + c.Request.Host
	}
	return base + "/s/" + token
}

// failShare maps share-domain errors to response codes.
func failShare(c *gin.Context, err error) {
	switch {
	case errors.Is(err, share.ErrNotFound):
		respond(c, serializer.Err(serializer.CodeNotFound, "share not found"))
	case errors.Is(err, share.ErrPasswordRequired), errors.Is(err, share.ErrWrongPassword):
		respond(c, serializer.Err(serializer.CodeUnauthorized, err.Error()))
	case errors.Is(err, share.ErrExpired), errors.Is(err, share.ErrExhausted), errors.Is(err, share.ErrNotAllowed):
		respond(c, serializer.Err(serializer.CodeForbidden, err.Error()))
	case errors.Is(err, share.ErrNotAFile):
		respond(c, serializer.Err(serializer.CodeBadRequest, err.Error()))
	default:
		respond(c, serializer.Err(serializer.CodeInternal, err.Error()))
	}
}
