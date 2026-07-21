package controllers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// apiTokenDTO is the JSON shape for a token (never its hash or plaintext).
type apiTokenDTO struct {
	ID         uint       `json:"id"`
	Label      string     `json:"label"`
	Prefix     string     `json:"prefix"`
	ReadOnly   bool       `json:"read_only"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toTokenDTO(t *model.APIToken) apiTokenDTO {
	return apiTokenDTO{
		ID:         t.ID,
		Label:      t.Label,
		Prefix:     t.Prefix,
		ReadOnly:   t.ReadOnly,
		ExpiresAt:  t.ExpiresAt,
		LastUsedAt: t.LastUsedAt,
		CreatedAt:  t.CreatedAt,
	}
}

// TokenList returns the current user's personal access tokens.
func (ctl *Controller) TokenList(c *gin.Context) {
	user := middleware.UserFrom(c)
	toks, err := ctl.dep.Repo.APIToken.ListByUser(c.Request.Context(), user.ID)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]apiTokenDTO, 0, len(toks))
	for i := range toks {
		out = append(out, toTokenDTO(&toks[i]))
	}
	respond(c, serializer.OK(gin.H{"tokens": out})) //nolint
}

// TokenCreate mints a token and returns the plaintext once (it is stored only as
// a hash and can never be shown again).
func (ctl *Controller) TokenCreate(c *gin.Context) {
	user := middleware.UserFrom(c)

	var req struct {
		Label       string `json:"label"`
		ReadOnly    bool   `json:"read_only"`
		ExpiresDays int    `json:"expires_days"`
	}
	_ = c.ShouldBindJSON(&req)
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "API token"
	}

	plain, prefix, hash := genAPIToken()
	tok := &model.APIToken{
		UserID:    user.ID,
		Label:     label,
		Prefix:    prefix,
		TokenHash: hash,
		ReadOnly:  req.ReadOnly,
	}
	if req.ExpiresDays > 0 {
		exp := time.Now().Add(time.Duration(req.ExpiresDays) * 24 * time.Hour)
		tok.ExpiresAt = &exp
	}
	if err := ctl.dep.Repo.APIToken.Create(c.Request.Context(), tok); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{"token_info": toTokenDTO(tok), "token": plain}))
}

// TokenDelete revokes one of the user's tokens.
func (ctl *Controller) TokenDelete(c *gin.Context) {
	user := middleware.UserFrom(c)
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := ctl.dep.Repo.APIToken.Delete(c.Request.Context(), user.ID, uint(id)); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// genAPIToken returns a new token: the plaintext ("od_" + base32 random), a
// display prefix, and the SHA-256 hash that is stored.
func genAPIToken() (plain, prefix, hash string) {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	plain = "od_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf))
	prefix = plain[:11]
	sum := sha256.Sum256([]byte(plain))
	return plain, prefix, hex.EncodeToString(sum[:])
}
