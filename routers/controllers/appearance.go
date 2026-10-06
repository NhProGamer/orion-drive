package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"unicode/utf8"

	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

const (
	// settingCustomCSS is the settings key holding the instance-wide custom CSS.
	settingCustomCSS = "custom_css"
	// maxCustomCSSBytes caps the stylesheet; it also stays under MySQL's 64 KiB
	// TEXT column limit.
	maxCustomCSSBytes = 64000
)

// SiteCustomCSS serves the admin-defined stylesheet, loaded by every page
// (drive and public share links). Public on purpose: share visitors are not
// signed in. Served as a real text/css file, rather than inlined, so a strict
// operator CSP (style-src 'self') keeps working. The ETag lets browsers
// revalidate cheaply on each load and pick up an edit immediately.
func (ctl *Controller) SiteCustomCSS(c *gin.Context) {
	css, err := ctl.dep.Repo.Setting.Get(c.Request.Context(), settingCustomCSS)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256([]byte(css))
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	c.Header("ETag", etag)
	c.Header("Cache-Control", "no-cache")
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, "text/css; charset=utf-8", []byte(css))
}

type appearanceDTO struct {
	CustomCSS string `json:"custom_css"`
}

// AdminGetAppearance returns the editable appearance settings.
func (ctl *Controller) AdminGetAppearance(c *gin.Context) {
	css, err := ctl.dep.Repo.Setting.Get(c.Request.Context(), settingCustomCSS)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(appearanceDTO{CustomCSS: css}))
}

// AdminUpdateAppearance replaces the instance-wide custom CSS. The content is
// trusted (admin-only, applied to everyone) and only bounded in size; an
// operator wanting to block external fetches from it should set a CSP with
// img-src/font-src 'self'.
func (ctl *Controller) AdminUpdateAppearance(c *gin.Context) {
	var req appearanceDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	if len(req.CustomCSS) > maxCustomCSSBytes {
		respond(c, serializer.Err(serializer.CodeBadRequest, "custom CSS too large"))
		return
	}
	if !utf8.ValidString(req.CustomCSS) {
		respond(c, serializer.Err(serializer.CodeBadRequest, "custom CSS must be UTF-8"))
		return
	}
	if err := ctl.dep.Repo.Setting.Set(c.Request.Context(), settingCustomCSS, req.CustomCSS); err != nil {
		fail(c, err)
		return
	}
	// Restyling every page (shares included) is a sensitive change: keep a trail.
	ctl.dep.Logger.Info("custom CSS updated", "admin", middleware.UserFrom(c).Email, "bytes", len(req.CustomCSS))
	respond(c, serializer.OK(nil))
}
