package controllers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/application/constants"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// directLinkDTO is the JSON shape for a direct link.
type directLinkDTO struct {
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	Downloads int       `json:"downloads"`
	Created   time.Time `json:"created"`
}

// directLinkURL builds the public URL for a token.
func (ctl *Controller) directLinkURL(token string) string {
	base := strings.TrimRight(ctl.dep.Config.System.SiteURL, "/")
	return fmt.Sprintf("%s%s/link/%s", base, constants.APIPrefix, token)
}

// CreateDirectLink creates a direct link for a file.
func (ctl *Controller) CreateDirectLink(c *gin.Context) {
	var req struct {
		ID uint `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	link, err := ctl.dep.Files.CreateDirectLink(c.Request.Context(), ctl.user(c), req.ID)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(directLinkDTO{
		Token:     link.Token,
		URL:       ctl.directLinkURL(link.Token),
		Downloads: link.Downloads,
		Created:   link.CreatedAt,
	}))
}

// ListDirectLinks returns a file's direct links.
func (ctl *Controller) ListDirectLinks(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	links, err := ctl.dep.Files.ListDirectLinks(c.Request.Context(), ctl.user(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]directLinkDTO, 0, len(links))
	for _, l := range links {
		out = append(out, directLinkDTO{
			Token:     l.Token,
			URL:       ctl.directLinkURL(l.Token),
			Downloads: l.Downloads,
			Created:   l.CreatedAt,
		})
	}
	respond(c, serializer.OK(out))
}

// DeleteDirectLink removes a direct link.
func (ctl *Controller) DeleteDirectLink(c *gin.Context) {
	if err := ctl.dep.Files.DeleteDirectLink(c.Request.Context(), ctl.user(c), c.Param("token")); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// DirectLinkContent serves a file's content over a public direct link.
func (ctl *Controller) DirectLinkContent(c *gin.Context) {
	target, err := ctl.dep.Files.DirectDownload(c.Request.Context(), c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	if target.URL != "" {
		c.Redirect(http.StatusFound, target.URL)
		return
	}
	defer target.Stream.Close()
	// Direct links are for embedding/hotlinking, so serve inline — but serveContent
	// only honours inline for viewer-safe types and forces a download otherwise.
	serveContent(c, target.File.Name, target.File.UpdatedAt, target.Stream, true)
}
