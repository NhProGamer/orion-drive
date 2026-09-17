// Package controllers implements the HTTP handlers for the OrionDrive API.
package controllers

import (
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// Controller carries the dependency container into each handler.
type Controller struct {
	dep *bootstrap.Dependency
}

// New builds a Controller.
func New(dep *bootstrap.Dependency) *Controller { return &Controller{dep: dep} }

// respond writes a serializer.Response with its mapped HTTP status.
func respond(c *gin.Context, r serializer.Response) {
	c.JSON(r.HTTPStatus(), r)
}

// fail maps a domain error to the appropriate response code.
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, filemanager.ErrNotFound):
		respond(c, serializer.Err(serializer.CodeNotFound, "not found"))
	case errors.Is(err, filemanager.ErrConflict):
		respond(c, serializer.Err(serializer.CodeConflict, err.Error()))
	case errors.Is(err, filemanager.ErrInvalidName):
		respond(c, serializer.Err(serializer.CodeBadRequest, err.Error()))
	case errors.Is(err, filemanager.ErrQuota):
		respond(c, serializer.Err(serializer.CodeForbidden, err.Error()))
	case errors.Is(err, filemanager.ErrLocked):
		respond(c, serializer.Err(serializer.CodeConflict, err.Error()))
	// Archive limits: the request is legitimate but larger than the server
	// allows, so say which limit it hit instead of returning a bare 500.
	case errors.Is(err, filemanager.ErrArchiveTooLarge),
		errors.Is(err, filemanager.ErrTooManyFiles):
		respond(c, serializer.Err(serializer.CodeForbidden, err.Error()))
	case errors.Is(err, filemanager.ErrArchiveTooDeep),
		errors.Is(err, filemanager.ErrArchiveFormat),
		errors.Is(err, filemanager.ErrNotAnArchive):
		respond(c, serializer.Err(serializer.CodeBadRequest, err.Error()))
	default:
		// Log the real error server-side but return a generic message: raw
		// internal errors leak DB/driver/filesystem detail to the client.
		slog.Error("request failed", "method", c.Request.Method, "path", c.Request.URL.Path, "error", err)
		respond(c, serializer.Err(serializer.CodeInternal, "internal server error"))
	}
}

// fileDTO is the JSON shape the frontend consumes for a file/folder.
type fileDTO struct {
	ID       uint      `json:"id"`
	ParentID *uint     `json:"parent_id"`
	Name     string    `json:"name"`
	Type     string    `json:"type"` // "file" | "folder"
	Size     int64     `json:"size"`
	Starred  bool      `json:"starred"`
	Locked   bool      `json:"locked"`
	Owner    string    `json:"owner"`
	Modified time.Time `json:"modified"`
	// Location is the "/"-joined ancestor folder path, set only for search
	// results (which span folders). Empty for items at the drive root.
	Location string `json:"location,omitempty"`
}

func toDTO(f *model.File, owner string) fileDTO {
	t := "file"
	if f.IsFolder() {
		t = "folder"
	}
	return fileDTO{
		ID:       f.ID,
		ParentID: f.ParentID,
		Name:     f.Name,
		Type:     t,
		Size:     f.Size,
		Starred:  f.Starred,
		Locked:   f.IsLocked(),
		Owner:    owner,
		Modified: f.UpdatedAt,
	}
}

func toDTOs(files []model.File, owner string) []fileDTO {
	out := make([]fileDTO, 0, len(files))
	for i := range files {
		out = append(out, toDTO(&files[i], owner))
	}
	return out
}

// user returns the authenticated user for the request.
func (ctl *Controller) user(c *gin.Context) *model.User { return middleware.UserFrom(c) }

// parseUint parses a base-10 uint from a string.
func parseUint(s string) (uint, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	return uint(n), err
}

// parseParentID reads an optional parent id: "", "0" and "root" mean the drive root.
func parseParentID(s string) (*uint, error) {
	if s == "" || s == "root" || s == "0" {
		return nil, nil
	}
	id, err := parseUint(s)
	if err != nil {
		return nil, err
	}
	return &id, nil
}
