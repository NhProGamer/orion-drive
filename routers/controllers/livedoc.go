package controllers

import (
	"context"
	"regexp"

	"github.com/NhProGamer/orion-drive/pkg/livedoc"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// markdownExt matches the file names the collaborative editor handles.
var markdownExt = regexp.MustCompile(`(?i)\.(md|markdown)$`)

// IsMarkdown reports whether a file name is a Markdown document.
func IsMarkdown(name string) bool { return markdownExt.MatchString(name) }

// DocSocket upgrades an authenticated request to the Markdown editing relay for
// one of the user's own files.
func (ctl *Controller) DocSocket(c *gin.Context) {
	if ctl.dep.Docs == nil {
		respond(c, serializer.Err(serializer.CodeForbidden, "live document editing is disabled"))
		return
	}
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	user := ctl.user(c)
	f, err := ctl.dep.Repo.File.GetByID(c.Request.Context(), user.ID, id)
	if err != nil {
		fail(c, err)
		return
	}
	if f.IsFolder() || !IsMarkdown(f.Name) {
		respond(c, serializer.Err(serializer.CodeBadRequest, "not a Markdown document"))
		return
	}
	ctl.dep.Docs.Serve(c.Writer, c.Request, livedoc.Session{
		FileID:   f.ID,
		OwnerID:  user.ID,
		CanWrite: true,
		Name:     user.DisplayName(),
	})
}

// DocSocketShare upgrades a public share request to the Markdown editing relay.
// The share token and (when set) its password travel as query parameters,
// because a browser WebSocket handshake cannot carry custom headers.
func (ctl *Controller) DocSocketShare(c *gin.Context) {
	if ctl.dep.Docs == nil {
		respond(c, serializer.Err(serializer.CodeForbidden, "live document editing is disabled"))
		return
	}
	token := c.Param("token")
	fileID, ownerID, canWrite, name, err := ctl.dep.Shares.LiveTarget(c.Request.Context(), token, c.Query("path"), c.Query("password"))
	if err != nil {
		failShare(c, err)
		return
	}
	if !IsMarkdown(name) {
		respond(c, serializer.Err(serializer.CodeBadRequest, "not a Markdown document"))
		return
	}
	ctl.dep.Docs.Serve(c.Writer, c.Request, livedoc.Session{
		FileID:   fileID,
		OwnerID:  ownerID,
		CanWrite: canWrite,
		Name:     peerName(c.Query("name")),
		// A share can be deleted, expire or be downgraded while a visitor keeps
		// the document open; re-checking it ends (or demotes) the live session
		// instead of letting the open tab keep writing.
		Revalidate: func(ctx context.Context) (bool, bool) {
			return ctl.dep.Shares.StillValid(ctx, token)
		},
	})
}
