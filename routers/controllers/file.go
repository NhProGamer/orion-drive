package controllers

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/gin-gonic/gin"
)

// ListFiles returns the contents of a folder, the trash, or search results.
// Query params: parent (id|root), view (drive|trash), q (search).
func (ctl *Controller) ListFiles(c *gin.Context) {
	u := ctl.user(c)
	ctx := c.Request.Context()
	owner := u.DisplayName()

	filters := repository.SearchFilters{
		Query:   c.Query("q"),
		Type:    c.Query("type"),
		Starred: c.Query("starred") == "1",
	}
	if v, err := strconv.ParseInt(c.Query("min_size"), 10, 64); err == nil {
		filters.MinSize = v
	}
	if v, err := strconv.ParseInt(c.Query("max_size"), 10, 64); err == nil {
		filters.MaxSize = v
	}
	if t, err := time.Parse(time.RFC3339, c.Query("after")); err == nil {
		filters.After = &t
	}
	if t, err := time.Parse(time.RFC3339, c.Query("before")); err == nil {
		filters.Before = &t
	}
	kind := c.Query("kind")
	// Enter search mode when a query or any filter is present.
	if filters.Query != "" || filters.Type != "" || filters.Starred ||
		filters.MinSize > 0 || filters.MaxSize > 0 || filters.After != nil ||
		filters.Before != nil || kind != "" {
		files, err := ctl.dep.Files.Search(ctx, u, filters, kind)
		if err != nil {
			fail(c, err)
			return
		}
		respond(c, serializer.OK(ctl.searchDTOs(ctx, u, files, owner)))
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

	if c.Query("all") == "1" {
		files, err := ctl.dep.Files.ListAllFiles(ctx, u)
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

// searchDTOs maps search results to DTOs, resolving each hit's location (the
// "/"-joined ancestor folder path) so the UI can show where a match lives.
func (ctl *Controller) searchDTOs(ctx context.Context, u *model.User, files []model.File, owner string) []fileDTO {
	folders, _ := ctl.dep.Repo.File.AllFolders(ctx, u.ID)
	type node struct {
		name   string
		parent *uint
	}
	byID := make(map[uint]node, len(folders))
	for i := range folders {
		byID[folders[i].ID] = node{folders[i].Name, folders[i].ParentID}
	}
	location := func(parent *uint) string {
		var parts []string
		for guard := 0; parent != nil && guard < 64; guard++ {
			n, ok := byID[*parent]
			if !ok {
				break
			}
			parts = append([]string{n.name}, parts...)
			parent = n.parent
		}
		return strings.Join(parts, "/")
	}
	out := make([]fileDTO, 0, len(files))
	for i := range files {
		d := toDTO(&files[i], owner)
		d.Location = location(files[i].ParentID)
		out = append(out, d)
	}
	return out
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

type ensureFolderPathReq struct {
	Parent string `json:"parent"`
	Path   string `json:"path"`
}

// EnsureFolderPath idempotently creates the folder chain given by a
// slash-separated relative path under parent, returning the leaf folder. Used by
// folder uploads to materialise the directory tree before storing files.
func (ctl *Controller) EnsureFolderPath(c *gin.Context) {
	var req ensureFolderPathReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	parentID, err := parseParentID(req.Parent)
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid parent"))
		return
	}
	var segments []string
	for _, s := range strings.Split(req.Path, "/") {
		if s = strings.TrimSpace(s); s != "" && s != "." && s != ".." {
			segments = append(segments, s)
		}
	}
	if len(segments) == 0 {
		respond(c, serializer.Err(serializer.CodeBadRequest, "empty path"))
		return
	}
	f, err := ctl.dep.Files.EnsureFolderPath(c.Request.Context(), ctl.user(c), parentID, segments)
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
// Ancestors returns the breadcrumb trail (root-first folders) for a file or
// folder, so the UI can rebuild navigation state from a bookmarked/refreshed URL.
func (ctl *Controller) Ancestors(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	crumbs, err := ctl.dep.Files.Ancestors(c.Request.Context(), ctl.user(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(crumbs))
}

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

// EmptyTrash permanently deletes every trashed file the user owns.
func (ctl *Controller) EmptyTrash(c *gin.Context) {
	n, err := ctl.dep.Files.EmptyTrash(c.Request.Context(), ctl.user(c))
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{"purged": n}))
}

// Download delivers a file's content: it redirects to a direct provider URL
// when the backend offers one, otherwise streams the (rate-limited) content.
func (ctl *Controller) Download(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	target, err := ctl.dep.Files.Download(c.Request.Context(), ctl.user(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	if target.URL != "" {
		c.Redirect(http.StatusFound, target.URL)
		return
	}
	defer target.Stream.Close()
	serveContent(c, target.File.Name, target.File.UpdatedAt, target.Stream, c.Query("inline") != "")
}

// extraContentTypes covers extensions the stdlib mime package may not map.
// inlineContentType guesses a Content-Type for inline previews.
// maxBlobSize caps an in-place binary save (e.g. an edited image).
const maxBlobSize = 100 << 20

// SaveBlob overwrites a file's binary content (raw request body), creating a new
// version. Used by the in-browser image editor.
func (ctl *Controller) SaveBlob(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBlobSize))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "read error"))
		return
	}
	f, err := ctl.dep.Files.SaveVersion(c.Request.Context(), ctl.user(c), id, body)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTO(f, ctl.user(c).DisplayName())))
}

// Thumbnail serves a JPEG thumbnail for a file (generated and cached on first
// request). Returns 404 when the file type has no thumbnail.
func (ctl *Controller) Thumbnail(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	data, err := ctl.dep.Files.Thumbnail(c.Request.Context(), ctl.user(c), id)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.Data(http.StatusOK, "image/jpeg", data)
}

// SaveText overwrites a text file's content, creating a new version.
func (ctl *Controller) SaveText(c *gin.Context) {
	var req struct {
		ID      uint   `json:"id"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	f, err := ctl.dep.Files.SaveVersion(c.Request.Context(), ctl.user(c), req.ID, []byte(req.Content))
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTO(f, ctl.user(c).DisplayName())))
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

// FolderSize returns the recursive total size of a folder's contents.
func (ctl *Controller) FolderSize(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	size, err := ctl.dep.Files.FolderSize(c.Request.Context(), ctl.user(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{"size": size}))
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
	mask, _ := ctl.dep.Files.UploadProgress(s.ID, s.NumChunks())
	respond(c, serializer.OK(gin.H{
		"session_id": s.ID,
		"chunk_size": s.ChunkSize,
		"num_chunks": s.NumChunks(),
		"received":   mask,
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
	mask, complete := ctl.dep.Files.UploadProgress(s.ID, s.NumChunks())
	respond(c, serializer.OK(gin.H{"received": mask, "complete": complete}))
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
