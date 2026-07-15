package controllers

import (
	"time"

	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// AdminStats returns dashboard counters.
func (ctl *Controller) AdminStats(c *gin.Context) {
	ctx := c.Request.Context()
	users, _ := ctl.dep.Repo.User.List(ctx)
	var storageUsed int64
	for i := range users {
		storageUsed += users[i].StorageUsed
	}
	files, _ := ctl.dep.Repo.File.CountAll(ctx)
	shares, _ := ctl.dep.Repo.Share.Count(ctx)
	groups, _ := ctl.dep.Repo.Group.List(ctx)
	policies, _ := ctl.dep.Repo.Policy.List(ctx)
	respond(c, serializer.OK(gin.H{
		"users":        len(users),
		"files":        files,
		"shares":       shares,
		"groups":       len(groups),
		"policies":     len(policies),
		"storage_used": storageUsed,
	}))
}

type adminUserDTO struct {
	ID          uint      `json:"id"`
	Email       string    `json:"email"`
	Nick        string    `json:"nick"`
	Status      int       `json:"status"`
	StorageUsed int64     `json:"storage_used"`
	GroupID     uint      `json:"group_id"`
	GroupName   string    `json:"group_name"`
	Admin       bool      `json:"admin"`
	CreatedAt   time.Time `json:"created_at"`
}

// AdminListUsers returns every user.
func (ctl *Controller) AdminListUsers(c *gin.Context) {
	users, err := ctl.dep.Repo.User.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	admins := ctl.dep.Config.System.AdminEmailSet()
	out := make([]adminUserDTO, 0, len(users))
	for i := range users {
		u := &users[i]
		name := ""
		if u.Group != nil {
			name = u.Group.Name
		}
		out = append(out, adminUserDTO{
			ID: u.ID, Email: u.Email, Nick: u.DisplayName(), Status: u.Status,
			StorageUsed: u.StorageUsed, GroupID: u.GroupID, GroupName: name,
			Admin: middleware.IsAdmin(u, admins), CreatedAt: u.CreatedAt,
		})
	}
	respond(c, serializer.OK(out))
}

// AdminUpdateUser changes a user's group and/or status.
func (ctl *Controller) AdminUpdateUser(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	var req struct {
		GroupID *uint `json:"group_id"`
		Status  *int  `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	ctx := c.Request.Context()
	u, err := ctl.dep.Repo.User.GetByID(ctx, id)
	if err != nil {
		fail(c, err)
		return
	}
	// Detach the preloaded association so GORM writes group_id from the field
	// rather than re-deriving it from u.Group.
	u.Group = nil
	if req.GroupID != nil {
		if _, err := ctl.dep.Repo.Group.GetByID(ctx, *req.GroupID); err != nil {
			respond(c, serializer.Err(serializer.CodeBadRequest, "unknown group"))
			return
		}
		u.GroupID = *req.GroupID
	}
	if req.Status != nil {
		u.Status = *req.Status
	}
	if err := ctl.dep.Repo.User.Update(ctx, u); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

type adminGroupDTO struct {
	ID              uint   `json:"id"`
	Name            string `json:"name"`
	MaxStorage      int64  `json:"max_storage"`
	SpeedLimit      int64  `json:"speed_limit"`
	StoragePolicyID uint   `json:"storage_policy_id"`
	CanShare        bool   `json:"can_share"`
	CanAdmin        bool   `json:"can_admin"`
	UserCount       int64  `json:"user_count"`
}

// AdminListGroups returns every group with its member count.
func (ctl *Controller) AdminListGroups(c *gin.Context) {
	ctx := c.Request.Context()
	groups, err := ctl.dep.Repo.Group.List(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]adminGroupDTO, 0, len(groups))
	for i := range groups {
		g := &groups[i]
		n, _ := ctl.dep.Repo.Group.CountUsers(ctx, g.ID)
		out = append(out, adminGroupDTO{
			ID: g.ID, Name: g.Name, MaxStorage: g.MaxStorage, SpeedLimit: g.SpeedLimit,
			StoragePolicyID: g.StoragePolicyID, CanShare: g.CanShare(), CanAdmin: g.CanAdmin(),
			UserCount: n,
		})
	}
	respond(c, serializer.OK(out))
}

type groupReq struct {
	Name            string `json:"name"`
	MaxStorage      int64  `json:"max_storage"`
	SpeedLimit      int64  `json:"speed_limit"`
	StoragePolicyID uint   `json:"storage_policy_id"`
	CanShare        bool   `json:"can_share"`
	CanAdmin        bool   `json:"can_admin"`
}

func (r groupReq) apply(g *model.Group) {
	g.Name = r.Name
	g.MaxStorage = r.MaxStorage
	g.SpeedLimit = r.SpeedLimit
	g.StoragePolicyID = r.StoragePolicyID
	share, admin := r.CanShare, r.CanAdmin
	g.Permissions = model.MustJSON(model.GroupPermissions{Share: &share, Admin: &admin})
}

// AdminCreateGroup creates a group.
func (ctl *Controller) AdminCreateGroup(c *gin.Context) {
	var req groupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	g := &model.Group{}
	req.apply(g)
	if err := ctl.dep.Repo.Group.Create(c.Request.Context(), g); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{"id": g.ID}))
}

// AdminUpdateGroup updates a group.
func (ctl *Controller) AdminUpdateGroup(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	var req groupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	ctx := c.Request.Context()
	g, err := ctl.dep.Repo.Group.GetByID(ctx, id)
	if err != nil {
		fail(c, err)
		return
	}
	req.apply(g)
	if err := ctl.dep.Repo.Group.Update(ctx, g); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// AdminDeleteGroup removes a group that has no members and is not the default.
func (ctl *Controller) AdminDeleteGroup(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	if id == 1 {
		respond(c, serializer.Err(serializer.CodeBadRequest, "the default group cannot be deleted"))
		return
	}
	ctx := c.Request.Context()
	if n, _ := ctl.dep.Repo.Group.CountUsers(ctx, id); n > 0 {
		respond(c, serializer.Err(serializer.CodeConflict, "group still has members"))
		return
	}
	if err := ctl.dep.Repo.Group.Delete(ctx, id); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}

// AdminListPolicies returns every storage policy.
func (ctl *Controller) AdminListPolicies(c *gin.Context) {
	policies, err := ctl.dep.Repo.Policy.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(policies))
	for i := range policies {
		p := &policies[i]
		out = append(out, gin.H{
			"id": p.ID, "name": p.Name, "type": p.Type, "server": p.Server,
			"bucket_name": p.BucketName, "base_path": p.BasePath, "settings": p.Settings,
		})
	}
	respond(c, serializer.OK(out))
}

// AdminCreatePolicy creates a storage policy (local/s3/remote).
func (ctl *Controller) AdminCreatePolicy(c *gin.Context) {
	var req struct {
		Name       string         `json:"name"`
		Type       string         `json:"type"`
		Server     string         `json:"server"`
		BucketName string         `json:"bucket_name"`
		BasePath   string         `json:"base_path"`
		AccessKey  string         `json:"access_key"`
		SecretKey  string         `json:"secret_key"`
		Settings   map[string]any `json:"settings"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid body"))
		return
	}
	switch req.Type {
	case model.PolicyTypeLocal, model.PolicyTypeS3, model.PolicyTypeRemote:
	default:
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid policy type"))
		return
	}
	p := &model.StoragePolicy{
		Name: req.Name, Type: req.Type, Server: req.Server, BucketName: req.BucketName,
		BasePath: req.BasePath, AccessKey: req.AccessKey, SecretKey: req.SecretKey,
	}
	if len(req.Settings) > 0 {
		p.Settings = model.MustJSON(req.Settings)
	}
	if err := ctl.dep.Repo.Policy.Create(c.Request.Context(), p); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(gin.H{"id": p.ID}))
}

// AdminDeletePolicy removes a storage policy not referenced by any group.
func (ctl *Controller) AdminDeletePolicy(c *gin.Context) {
	id, err := parseUint(c.Param("id"))
	if err != nil {
		respond(c, serializer.Err(serializer.CodeBadRequest, "invalid id"))
		return
	}
	ctx := c.Request.Context()
	if n, _ := ctl.dep.Repo.Policy.GroupsUsing(ctx, id); n > 0 {
		respond(c, serializer.Err(serializer.CodeConflict, "policy is still used by a group"))
		return
	}
	if err := ctl.dep.Repo.Policy.Delete(ctx, id); err != nil {
		fail(c, err)
		return
	}
	respond(c, serializer.OK(nil))
}
