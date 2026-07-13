package controllers

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver/remote"
	"github.com/gin-gonic/gin"
)

// SlaveController serves the slave storage protocol backed by a local store.
// Requests are already authenticated by the SlaveAuth middleware.
type SlaveController struct {
	store driver.Handler
}

// NewSlave builds a slave controller over the given storage backend.
func NewSlave(store driver.Handler) *SlaveController {
	return &SlaveController{store: store}
}

// Upload stores the request body at the given path.
func (s *SlaveController) Upload(c *gin.Context) {
	src := c.Query(remote.ParamPath)
	if src == "" {
		c.String(http.StatusBadRequest, "missing path")
		return
	}
	if err := s.store.Put(c.Request.Context(), src, c.Request.Body, c.Request.ContentLength); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// Content streams an object back (internal fetch).
func (s *SlaveController) Content(c *gin.Context) {
	s.serve(c, "")
}

// Download streams an object to an end client as an attachment.
func (s *SlaveController) Download(c *gin.Context) {
	name := c.Query(remote.ParamName)
	if name == "" {
		name = "download"
	}
	s.serve(c, name)
}

// serve opens the object and writes it with range support. When name is set the
// response is an attachment with that file name.
func (s *SlaveController) serve(c *gin.Context, name string) {
	src := c.Query(remote.ParamPath)
	if src == "" {
		c.String(http.StatusBadRequest, "missing path")
		return
	}
	rc, err := s.store.Open(c.Request.Context(), src)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	defer rc.Close()
	if name != "" {
		c.Header("Content-Disposition",
			fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(name)))
	}
	http.ServeContent(c.Writer, c.Request, name, time.Time{}, rc)
}

// Delete removes the requested objects and reports which keys failed.
func (s *SlaveController) Delete(c *gin.Context) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	failed, _ := s.store.Delete(c.Request.Context(), req.Paths...)
	c.JSON(http.StatusOK, gin.H{"failed": failed})
}
