package controllers

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ListFiles returns the contents of a folder, the trash, or search results.
// Query params: parent (id|root), view (drive|trash), q (search).
func (ctl *Controller) ListFiles(c *gin.Context) {
	u := ctl.user(c)
	ctx := c.Request.Context()
	owner := u.DisplayName()

	if q := c.Query("q"); q != "" {
		files, err := ctl.dep.Files.Search(ctx, u, q)
		if err != nil {
			fail(c, err)
			return
		}
		respond(c, serializer.OK(toDTOs(files, owner)))
		return
	}

	if c.Query("view") == "trash" {
		files, err := ctl.dep.Files.ListTrashed(ctx, u)
		if err != nil {
			fail(c, err)
			return
		}
		respond(c, serializer.OK(toDTOs(files, owner)))
		return
	}

	parentID, err := parseParentID(c.Query("parent"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid parent"))
		return
	}
	files, err := ctl.dep.Files.List(ctx, u, parentID)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTOs(files, owner)))
}

type createFolderReq struct {
	Parent string `json:"parent"`
	Name   string `json:"name"`
}

// CreateFolder creates a new folder.
func (ctl *Controller) CreateFolder(c *gin.Context) {
	var req createFolderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	parentID, err := parseParentID(req.Parent)
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid parent"))
		return
	}
	f, err := ctl.dep.Files.CreateFolder(c.Request.Context(), ctl.user(c), parentID, req.Name)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTO(f, ctl.user(c).DisplayName())))
}

type renameReq struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// Rename renames a file or folder.
func (ctl *Controller) Rename(c *gin.Context) {
	var req renameReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	f, err := ctl.dep.Files.Rename(c.Request.Context(), ctl.user(c), req.ID, req.Name)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTO(f, ctl.user(c).DisplayName())))
}

type moveReq struct {
	IDs    []uint `json:"ids"`
	Parent string `json:"parent"`
}

// Move relocates files under a new parent.
func (ctl *Controller) Move(c *gin.Context) {
	var req moveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	parentID, err := parseParentID(req.Parent)
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid parent"))
		return
	}
	if err := ctl.dep.Files.Move(c.Request.Context(), ctl.user(c), req.IDs, parentID); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// Star toggles the starred flag on a file.
func (ctl *Controller) Star(c *gin.Context) {
	var req struct {
		ID uint `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	f, err := ctl.dep.Files.ToggleStar(c.Request.Context(), ctl.user(c), req.ID)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTO(f, ctl.user(c).DisplayName())))
}

type idsReq struct {
	IDs []uint `json:"ids"`
}

// Trash moves files to the recycle bin.
func (ctl *Controller) Trash(c *gin.Context) {
	var req idsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	if err := ctl.dep.Files.Trash(c.Request.Context(), ctl.user(c), req.IDs); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// Restore returns files from the recycle bin.
func (ctl *Controller) Restore(c *gin.Context) {
	var req idsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	if err := ctl.dep.Files.Restore(c.Request.Context(), ctl.user(c), req.IDs); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// Purge permanently deletes files.
func (ctl *Controller) Purge(c *gin.Context) {
	var req idsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	if err := ctl.dep.Files.Purge(c.Request.Context(), ctl.user(c), req.IDs); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// Download streams a file's content to the client.
func (ctl *Controller) Download(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	rc, f, err := ctl.dep.Files.Download(c.Request.Context(), ctl.user(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	defer rc.Close()

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(f.Name)))
	c.Header("Content-Type", "application/octet-stream")
	http.ServeContent(c.Writer, c.Request, f.Name, f.UpdatedAt, rc)
}

// Capacity returns the user's storage usage and quota.
func (ctl *Controller) Capacity(c *gin.Context) {
	used, total, err := ctl.dep.Files.Capacity(c.Request.Context(), ctl.user(c))
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{"used": used, "total": total}))
}

type initUploadReq struct {
	Parent string `json:"parent"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
}

// InitUpload opens a resumable upload session.
func (ctl *Controller) InitUpload(c *gin.Context) {
	var req initUploadReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	parentID, err := parseParentID(req.Parent)
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid parent"))
		return
	}
	s, err := ctl.dep.Files.InitUpload(c.Request.Context(), ctl.user(c), parentID, req.Name, req.Size)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{
		"session_id": s.ID,
		"chunk_size": s.ChunkSize,
		"num_chunks": s.NumChunks(),
		"received":   s.Received,
	}))
}

// PutChunk receives one chunk of an upload. The chunk index is passed as the
// X-Chunk-Index header; the body is the raw chunk bytes.
func (ctl *Controller) PutChunk(c *gin.Context) {
	sid := c.Param("sid")
	index, err := strconv.Atoi(c.GetHeader("X-Chunk-Index"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "missing X-Chunk-Index"))
		return
	}
	s, err := ctl.dep.Files.PutChunk(c.Request.Context(), ctl.user(c), sid, index, c.Request.Body)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{"received": s.Received, "complete": s.Complete()}))
}

// CompleteUpload finalizes an upload and returns the new file.
func (ctl *Controller) CompleteUpload(c *gin.Context) {
	sid := c.Param("sid")
	f, err := ctl.dep.Files.CompleteUpload(c.Request.Context(), ctl.user(c), sid)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTO(f, ctl.user(c).DisplayName())))
}

// CancelUpload aborts an in-progress upload.
func (ctl *Controller) CancelUpload(c *gin.Context) {
	if err := ctl.dep.Files.CancelUpload(ctl.user(c), c.Param("sid")); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}
