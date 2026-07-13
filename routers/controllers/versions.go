package controllers

import (
	"time"

	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// versionDTO is the JSON shape for one stored revision of a file.
type versionDTO struct {
	ID        uint      `json:"id"`
	Size      int64     `json:"size"`
	Created   time.Time `json:"created"`
	Current   bool      `json:"current"`
	Encrypted bool      `json:"encrypted"`
}

// ListVersions returns a file's version history.
func (ctl *Controller) ListVersions(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	_, versions, err := ctl.dep.Files.ListVersions(c.Request.Context(), ctl.user(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]versionDTO, 0, len(versions))
	for _, v := range versions {
		out = append(out, versionDTO{
			ID:        v.Entity.ID,
			Size:      v.Entity.Size,
			Created:   v.Entity.CreatedAt,
			Current:   v.Current,
			Encrypted: v.Entity.Encrypted(),
		})
	}
	respond(c, serializer.OK(out))
}

type versionRef struct {
	FileID   uint `json:"file_id"`
	EntityID uint `json:"entity_id"`
}

// RestoreVersion makes an older version current.
func (ctl *Controller) RestoreVersion(c *gin.Context) {
	var req versionRef
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	f, err := ctl.dep.Files.RestoreVersion(c.Request.Context(), ctl.user(c), req.FileID, req.EntityID)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTO(f, ctl.user(c).DisplayName())))
}

// DeleteVersion removes a non-current version.
func (ctl *Controller) DeleteVersion(c *gin.Context) {
	var req versionRef
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	if err := ctl.dep.Files.DeleteVersion(c.Request.Context(), ctl.user(c), req.FileID, req.EntityID); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}
