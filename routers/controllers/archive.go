package controllers

import (
	"net/http"
	"strings"

	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ArchiveDownload streams a ZIP of the requested files/folders. Ids are passed
// as a comma-separated `ids` query param so the browser can open the URL.
func (ctl *Controller) ArchiveDownload(c *gin.Context) {
	ids := parseIDList(c.Query("ids"))
	if len(ids) == 0 {
		respond(c, serializer.Err(serializer.CodeBadRequest, "no ids"))
		return
	}
	c.Header("Content-Disposition", `attachment; filename="orion-archive.zip"`)
	c.Header("Content-Type", "application/zip")
	if err := ctl.dep.Files.WriteArchive(c.Request.Context(), ctl.user(c), ids, c.Writer); err != nil {
		// Headers/stream may already be committed; surface the error for logs.
		_ = c.Error(err)
		c.Status(http.StatusInternalServerError)
	}
}

// parseIDList parses "1,2,3" into a slice of uints, skipping invalid entries.
func parseIDList(s string) []uint {
	var out []uint
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if id, err := parseUint(part); err == nil {
			out = append(out, id)
		}
	}
	return out
}
