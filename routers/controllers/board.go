package controllers

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/NhProGamer/orion-drive/pkg/board"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// BoardExt is the file extension of a collaborative whiteboard.
const BoardExt = ".excalidraw"

// maxPeerNameRunes bounds the display name a visitor may pick. It is shown to
// every other participant, so it is length-capped and stripped of control
// characters before it goes anywhere near another browser.
const maxPeerNameRunes = 32

// IsBoard reports whether a file name is a collaborative whiteboard.
func IsBoard(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), BoardExt)
}

// BoardSocket upgrades an authenticated request to the whiteboard relay for one
// of the user's own files.
func (ctl *Controller) BoardSocket(c *gin.Context) {
	if ctl.dep.Boards == nil {
		respond(c, serializer.Err(serializer.CodeForbidden, "whiteboard collaboration is disabled"))
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
	if f.IsFolder() || !IsBoard(f.Name) {
		respond(c, serializer.Err(serializer.CodeBadRequest, "not a whiteboard"))
		return
	}
	ctl.dep.Boards.Serve(c.Writer, c.Request, board.Session{
		FileID:   f.ID,
		OwnerID:  user.ID,
		CanWrite: true,
		Name:     user.DisplayName(),
	})
}

// BoardSocketShare upgrades a public share request to the whiteboard relay. The
// share token and (when set) its password travel as query parameters because a
// browser WebSocket handshake cannot carry custom headers.
func (ctl *Controller) BoardSocketShare(c *gin.Context) {
	if ctl.dep.Boards == nil {
		respond(c, serializer.Err(serializer.CodeForbidden, "whiteboard collaboration is disabled"))
		return
	}
	token := c.Param("token")
	password := c.Query("password")
	fileID, ownerID, canWrite, name, err := ctl.dep.Shares.LiveTarget(c.Request.Context(), token, c.Query("path"), password)
	if err != nil {
		failShare(c, err)
		return
	}
	if !IsBoard(name) {
		respond(c, serializer.Err(serializer.CodeBadRequest, "not a whiteboard"))
		return
	}
	ctl.dep.Boards.Serve(c.Writer, c.Request, board.Session{
		FileID:   fileID,
		OwnerID:  ownerID,
		CanWrite: canWrite,
		Name:     peerName(c.Query("name")),
		// A share can be deleted, expire or be downgraded while a visitor keeps
		// the board open; re-checking it ends (or demotes) the live session
		// instead of letting the open tab draw on indefinitely.
		Revalidate: func(ctx context.Context) (bool, bool) {
			return ctl.dep.Shares.StillValid(ctx, token)
		},
	})
}

// NewBoard creates an empty whiteboard in one of the user's folders.
func (ctl *Controller) NewBoard(c *gin.Context) {
	var req struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
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
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Tableau blanc"
	}
	if !IsBoard(name) {
		name += BoardExt
	}
	f, err := ctl.dep.Files.CreateFile(c.Request.Context(), ctl.user(c), parentID, name, board.EmptyScene())
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(toDTO(f, ctl.user(c).DisplayName())))
}

// peerName sanitises the display name a share visitor picked: control
// characters are dropped and the result is truncated, so a participant list
// cannot be used to inject markup or flood the other editors' screens.
func peerName(raw string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, raw)
	cleaned = strings.TrimSpace(cleaned)
	if utf8.RuneCountInString(cleaned) > maxPeerNameRunes {
		runes := []rune(cleaned)
		cleaned = string(runes[:maxPeerNameRunes])
	}
	return cleaned
}
