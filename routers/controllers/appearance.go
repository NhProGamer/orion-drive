package controllers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/NhProGamer/orion-drive/application/constants"
	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/NhProGamer/orion-drive/repository"
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

// maxSiteAssetBytes caps an uploaded branding image.
const maxSiteAssetBytes = 512 << 10

// siteAssetNames are the branding images an admin may replace.
var siteAssetNames = map[string]bool{"favicon": true, "banner-light": true, "banner-dark": true}

// siteAssetCSP is sent with every branding image. An <img> never runs an SVG's
// scripts, but opening the asset URL directly would, on our origin: the
// sandbox makes that navigation script-less and origin-less.
const siteAssetCSP = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

// SiteBranding lists the custom branding images, as name → URL. The URL
// carries the content hash, so an upload changes it and busts caches. Missing
// names mean "use the built-in image". Public: share pages show the banner.
func (ctl *Controller) SiteBranding(c *gin.Context) {
	assets, err := ctl.dep.Repo.SiteAsset.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	out := gin.H{}
	for _, a := range assets {
		out[a.Name] = constants.APIPrefix + "/site/asset/" + a.Name + "?v=" + a.ETag
	}
	respond(c, serializer.OK(out))
}

// SiteAsset serves one custom branding image.
func (ctl *Controller) SiteAsset(c *gin.Context) {
	a, err := ctl.dep.Repo.SiteAsset.Get(c.Request.Context(), c.Param("name"))
	if errors.Is(err, repository.ErrNotFound) {
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	etag := `"` + a.ETag + `"`
	c.Header("ETag", etag)
	// SiteBranding hands out URLs carrying the content hash (?v=): such a URL
	// always means these bytes, so the browser may keep it forever and never
	// ask again. Any other URL (none or a stale hash) must revalidate.
	if c.Query("v") == a.ETag {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		c.Header("Cache-Control", "no-cache")
	}
	c.Header("Content-Security-Policy", siteAssetCSP)
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, a.ContentType, a.Data)
}

// AdminPutSiteAsset replaces a branding image with the raw request body. The
// type is sniffed from the bytes, never taken from the client.
func (ctl *Controller) AdminPutSiteAsset(c *gin.Context) {
	name := c.Param("name")
	if !siteAssetNames[name] {
		respond(c, serializer.Err(serializer.CodeBadRequest, "unknown asset"))
		return
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, maxSiteAssetBytes+1))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	if len(data) > maxSiteAssetBytes {
		respond(c, serializer.Err(serializer.CodeBadRequest, "image too large (512 KB max)"))
		return
	}
	ct, ok := sniffImageType(data)
	if !ok {
		respond(c, serializer.Err(serializer.CodeBadRequest, "unsupported image type (PNG, JPEG, WebP, ICO or SVG)"))
		return
	}
	sum := sha256.Sum256(data)
	a := &model.SiteAsset{Name: name, ContentType: ct, ETag: hex.EncodeToString(sum[:8]), Data: data, UpdatedAt: time.Now()}
	if err := ctl.dep.Repo.SiteAsset.Put(c.Request.Context(), a); err != nil {
		fail(c, err)
		return
	}
	ctl.dep.Logger.Info("branding image updated", "admin", middleware.UserFrom(c).Email, "name", name, "type", ct, "bytes", len(data))
	respond(c, serializer.OK(nil))
}

// AdminDeleteSiteAsset restores the built-in image for a branding slot.
func (ctl *Controller) AdminDeleteSiteAsset(c *gin.Context) {
	name := c.Param("name")
	if !siteAssetNames[name] {
		respond(c, serializer.Err(serializer.CodeBadRequest, "unknown asset"))
		return
	}
	if err := ctl.dep.Repo.SiteAsset.Delete(c.Request.Context(), name); err != nil {
		fail(c, err)
		return
	}
	ctl.dep.Logger.Info("branding image reset", "admin", middleware.UserFrom(c).Email, "name", name)
	respond(c, serializer.OK(nil))
}

// sniffImageType returns the content type of an accepted image format.
func sniffImageType(data []byte) (string, bool) {
	ct := http.DetectContentType(data)
	switch ct {
	case "image/png", "image/jpeg", "image/webp", "image/x-icon":
		return ct, true
	}
	// SVG is text to the sniffer (text/xml or text/plain): accept it only when
	// it actually holds an <svg> root.
	if strings.HasPrefix(ct, "text/") && bytes.Contains(bytes.ToLower(data), []byte("<svg")) {
		return "image/svg+xml", true
	}
	return "", false
}
