package controllers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/NhProGamer/orion-drive/pkg/archive"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ArchiveDownload streams an archive of the requested files/folders. Ids are
// passed as a comma-separated `ids` query param so the browser can open the
// URL, and `format` picks what to produce (default: the configured one).
func (ctl *Controller) ArchiveDownload(c *gin.Context) {
	ids := parseIDList(c.Query("ids"))
	if len(ids) == 0 {
		respond(c, serializer.Err(serializer.CodeBadRequest, "no ids"))
		return
	}
	format, err := ctl.dep.Files.ArchiveFormat(c.Query("format"))
	if err != nil {
		fail(c, err)
		return
	}
	level, _ := strconv.Atoi(c.Query("level"))
	// Measured first: a streaming response cannot take its headers back, so an
	// archive over the limits has to be refused before the download starts
	// rather than abandoned halfway through it.
	if _, err := ctl.dep.Files.PlanArchive(c.Request.Context(), ctl.user(c), ids); err != nil {
		fail(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "orion-archive"+archive.Extension(format)))
	c.Header("Content-Type", "application/octet-stream")
	if err := ctl.dep.Files.WriteArchive(c.Request.Context(), ctl.user(c), ids, c.Writer, format, level, nil); err != nil {
		// Headers/stream may already be committed; surface the error for logs.
		_ = c.Error(err)
		c.Status(http.StatusInternalServerError)
	}
}

// CompressArchive schedules a background job that zips files into a new archive.
func (ctl *Controller) CompressArchive(c *gin.Context) {
	var req struct {
		Parent string `json:"parent"`
		IDs    []uint `json:"ids"`
		Name   string `json:"name"`
		Format string `json:"format"`
		// Level is the compression effort, 1 (fastest) to 9 (smallest); 0
		// leaves the server's configured default.
		Level int `json:"level"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	parentID, err := parseParentID(req.Parent)
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid parent"))
		return
	}
	job, err := ctl.dep.Files.Compress(c.Request.Context(), ctl.user(c), parentID, req.IDs, req.Name, req.Format, req.Level)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(job))
}

// ExtractArchive schedules a background job that unpacks an archive file.
func (ctl *Controller) ExtractArchive(c *gin.Context) {
	var req struct {
		ID     uint   `json:"id"`
		Parent string `json:"parent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	parentID, err := parseParentID(req.Parent)
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid parent"))
		return
	}
	job, err := ctl.dep.Files.Extract(c.Request.Context(), ctl.user(c), req.ID, parentID)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(job))
}

// ArchiveFormats lists the formats this server can create, for the UI's
// chooser, with the one it defaults to.
func (ctl *Controller) ArchiveFormats(c *gin.Context) {
	format, err := ctl.dep.Files.ArchiveFormat("")
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{
		"formats": archive.Creatable,
		"default": format,
		"level":   ctl.dep.Files.ArchiveLevel(0),
	}))
}

// ArchiveEntries lists the contents of an archive file without extracting it.
func (ctl *Controller) ArchiveEntries(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	entries, err := ctl.dep.Files.ListArchiveEntries(c.Request.Context(), ctl.user(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(entries))
}

// TaskStatus returns one background job owned by the user.
func (ctl *Controller) TaskStatus(c *gin.Context) {
	job, ok := ctl.dep.Tasks.Get(ctl.user(c).ID, c.Param("id"))
	if !ok {
		respond(c, serializer.Err(serializer.CodeNotFound, "task not found"))
		return
	}
	respond(c, serializer.OK(job))
}

// TaskList returns the user's background jobs.
func (ctl *Controller) TaskList(c *gin.Context) {
	respond(c, serializer.OK(ctl.dep.Tasks.ListByUser(ctl.user(c).ID)))
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
