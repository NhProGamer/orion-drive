package controllers

import (
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// Lock locks a file, protecting it from modification.
func (ctl *Controller) Lock(c *gin.Context) { ctl.setLock(c, true) }

// Unlock removes a file's lock.
func (ctl *Controller) Unlock(c *gin.Context) { ctl.setLock(c, false) }

func (ctl *Controller) setLock(c *gin.Context, lock bool) {
	var req struct {
		ID uint `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	u := ctl.user(c)
	ctx := c.Request.Context()

	var (
		f   *model.File
		err error
	)
	if lock {
		f, err = ctl.dep.Files.Lock(ctx, u, req.ID)
	} else {
		f, err = ctl.dep.Files.Unlock(ctx, u, req.ID)
	}
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTO(f, u.DisplayName())))
}
