package controllers

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	xwebdav "github.com/NhProGamer/orion-drive/pkg/webdav"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// webdavAccountDTO is the JSON shape for a WebDAV credential (never its hash).
type webdavAccountDTO struct {
	ID         uint       `json:"id"`
	Label      string     `json:"label"`
	Username   string     `json:"username"`
	ReadOnly   bool       `json:"read_only"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toWebdavDTO(a *model.WebDAVAccount) webdavAccountDTO {
	return webdavAccountDTO{
		ID:         a.ID,
		Label:      a.Label,
		Username:   a.Username,
		ReadOnly:   a.ReadOnly,
		LastUsedAt: a.LastUsedAt,
		CreatedAt:  a.CreatedAt,
	}
}

// WebdavList returns the current user's WebDAV credentials.
func (ctl *Controller) WebdavList(c *gin.Context) {
	user := middleware.UserFrom(c)
	accs, err := ctl.dep.Repo.WebDAV.ListByUser(c.Request.Context(), user.ID)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]webdavAccountDTO, 0, len(accs))
	for i := range accs {
		out = append(out, toWebdavDTO(&accs[i]))
	}
	respond(c, serializer.OK(gin.H{"accounts": out, "url": ctl.davURL(), "sftp": ctl.sftpInfo(c)}))
}

// sftpInfo reports whether SFTP is enabled and where to reach it (same host as
// the web request, port from config), so the UI can show a connection string.
func (ctl *Controller) sftpInfo(c *gin.Context) gin.H {
	s := ctl.dep.Config.SFTP
	host := c.Request.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	port := 22
	if _, p, err := net.SplitHostPort(s.Listen); err == nil {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	return gin.H{"enabled": s.Enable, "host": host, "port": port}
}

// WebdavCreate provisions a new WebDAV credential and returns the generated
// password once (it is stored only as a hash and can never be shown again).
func (ctl *Controller) WebdavCreate(c *gin.Context) {
	user := middleware.UserFrom(c)

	var req struct {
		Label    string `json:"label"`
		ReadOnly bool   `json:"read_only"`
	}
	_ = c.ShouldBindJSON(&req)
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "WebDAV"
	}

	username := genUsername(user)
	password := genPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fail(c, err)
		return
	}

	acc := &model.WebDAVAccount{
		UserID:       user.ID,
		Label:        label,
		Username:     username,
		PasswordHash: string(hash),
		ReadOnly:     req.ReadOnly,
	}
	if err := ctl.dep.Repo.WebDAV.Create(c.Request.Context(), acc); err != nil {
		fail(c, err)
		return
	}

	respond(c, serializer.OK(gin.H{
		"account":  toWebdavDTO(acc),
		"password": password,
		"url":      ctl.davURL(),
	}))
}

// WebdavDelete revokes one of the user's WebDAV credentials.
func (ctl *Controller) WebdavDelete(c *gin.Context) {
	user := middleware.UserFrom(c)
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := ctl.dep.Repo.WebDAV.Delete(c.Request.Context(), user.ID, uint(id)); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// davURL is the base WebDAV URL clients connect to.
func (ctl *Controller) davURL() string {
	base := strings.TrimRight(ctl.dep.Config.System.SiteURL, "/")
	return base + xwebdav.Prefix + "/"
}

// genUsername builds a unique, client-friendly WebDAV username from the user's
// email local-part plus a random suffix (the unique index guards collisions).
func genUsername(user *model.User) string {
	local := user.Email
	if i := strings.IndexByte(local, '@'); i >= 0 {
		local = local[:i]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(local) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	base := b.String()
	if base == "" {
		base = "dav"
	}
	suf := make([]byte, 4)
	_, _ = rand.Read(suf)
	return base + "-" + hex.EncodeToString(suf)
}

// genPassword returns a 32-character base32 (no padding) random secret.
func genPassword() string {
	buf := make([]byte, 20)
	_, _ = rand.Read(buf)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf))
}
