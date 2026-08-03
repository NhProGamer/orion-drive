package controllers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// makeAdmin flips a user's group to carry the admin permission and reloads the
// user so u.Group.CanAdmin() is true.
func makeAdmin(t *testing.T, ctl *Controller, u *model.User) *model.User {
	t.Helper()
	ctx := context.Background()
	g, err := ctl.dep.Repo.Group.GetByID(ctx, u.GroupID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	yes := true
	g.Permissions = model.MustJSON(model.GroupPermissions{Admin: &yes})
	if err := ctl.dep.Repo.Group.Update(ctx, g); err != nil {
		t.Fatalf("update group: %v", err)
	}
	reloaded, err := ctl.dep.Repo.User.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return reloaded
}

func TestAdminListUsers(t *testing.T) {
	ctl, _, user := ogEnv(t)
	c, w := authReq(user, "GET", "/admin/users", "")
	ctl.AdminListUsers(c)
	e := decode(t, w)
	if e.Code != serializer.CodeOK {
		t.Fatalf("AdminListUsers code = %d (%s)", e.Code, w.Body.String())
	}
	var users []adminUserDTO
	if err := json.Unmarshal(e.Data, &users); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(users) != 1 || users[0].Email != user.Email {
		t.Fatalf("users = %+v, want the one seeded user", users)
	}
	// Default group has no admin permission, and no admin emails are configured.
	if users[0].Admin {
		t.Fatalf("seeded user should not be flagged admin")
	}
}

func TestAdminListGroupsAndPolicies(t *testing.T) {
	ctl, _, user := ogEnv(t)

	cg, wg := authReq(user, "GET", "/admin/groups", "")
	ctl.AdminListGroups(cg)
	eg := decode(t, wg)
	if eg.Code != serializer.CodeOK {
		t.Fatalf("AdminListGroups code = %d (%s)", eg.Code, wg.Body.String())
	}
	var groups []adminGroupDTO
	if err := json.Unmarshal(eg.Data, &groups); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	if len(groups) == 0 {
		t.Fatalf("expected at least the default group")
	}
	// The default group (id 1) must report one member (the seeded user).
	var def *adminGroupDTO
	for i := range groups {
		if groups[i].ID == 1 {
			def = &groups[i]
		}
	}
	if def == nil || def.UserCount != 1 {
		t.Fatalf("default group member count wrong: %+v", def)
	}

	cp, wp := authReq(user, "GET", "/admin/policies", "")
	ctl.AdminListPolicies(cp)
	ep := decode(t, wp)
	if ep.Code != serializer.CodeOK {
		t.Fatalf("AdminListPolicies code = %d (%s)", ep.Code, wp.Body.String())
	}
	var policies []map[string]any
	if err := json.Unmarshal(ep.Data, &policies); err != nil {
		t.Fatalf("decode policies: %v", err)
	}
	if len(policies) == 0 {
		t.Fatalf("expected a seeded storage policy")
	}
}

func TestAdminUpdateUser(t *testing.T) {
	ctl, _, user := ogEnv(t)
	ctx := context.Background()

	// Create a second group to move the user into.
	g := &model.Group{Name: "movers"}
	if err := ctl.dep.Repo.Group.Create(ctx, g); err != nil {
		t.Fatalf("create group: %v", err)
	}

	// Change both the group and the status.
	c, w := authReq(user, "PATCH", "/admin/users/"+utoa(user.ID),
		`{"group_id":`+utoa(g.ID)+`,"status":1}`)
	c.Params = gin.Params{{Key: "id", Value: utoa(user.ID)}}
	ctl.AdminUpdateUser(c)
	if e := decode(t, w); e.Code != serializer.CodeOK {
		t.Fatalf("AdminUpdateUser code = %d (%s)", e.Code, w.Body.String())
	}
	got, _ := ctl.dep.Repo.User.GetByID(ctx, user.ID)
	if got.GroupID != g.ID {
		t.Fatalf("group_id = %d, want %d", got.GroupID, g.ID)
	}
	if got.Status != 1 {
		t.Fatalf("status = %d, want 1", got.Status)
	}

	// An unknown group is rejected.
	c2, w2 := authReq(user, "PATCH", "/admin/users/"+utoa(user.ID), `{"group_id":9999}`)
	c2.Params = gin.Params{{Key: "id", Value: utoa(user.ID)}}
	ctl.AdminUpdateUser(c2)
	if e := decode(t, w2); e.Code != serializer.CodeBadRequest {
		t.Fatalf("unknown group code = %d, want %d", e.Code, serializer.CodeBadRequest)
	}
}

func TestAdminRunMaintenance(t *testing.T) {
	ctl, _, user := ogEnv(t)
	c, w := authReq(user, "POST", "/admin/maintenance", "")
	ctl.AdminRunMaintenance(c)
	e := decode(t, w)
	if e.Code != serializer.CodeOK {
		t.Fatalf("AdminRunMaintenance code = %d (%s)", e.Code, w.Body.String())
	}
	var body struct {
		PurgedTrash    int `json:"purged_trash"`
		CleanedUploads int `json:"cleaned_uploads"`
	}
	if err := json.Unmarshal(e.Data, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Nothing to purge on a fresh DB, but the counters must be present (>= 0).
	if body.PurgedTrash != 0 {
		t.Fatalf("purged_trash = %d, want 0 on fresh db", body.PurgedTrash)
	}
}

func TestAdminStats(t *testing.T) {
	ctl, _, user := ogEnv(t)
	c, w := authReq(user, "GET", "/admin/stats", "")
	ctl.AdminStats(c)
	e := decode(t, w)
	if e.Code != serializer.CodeOK {
		t.Fatalf("AdminStats code = %d (%s)", e.Code, w.Body.String())
	}
	var s struct {
		Users  int `json:"users"`
		Groups int `json:"groups"`
	}
	if err := json.Unmarshal(e.Data, &s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.Users != 1 {
		t.Fatalf("stats.users = %d, want 1", s.Users)
	}
	if s.Groups < 1 {
		t.Fatalf("stats.groups = %d, want >= 1", s.Groups)
	}
}

// TestRequireAdminGate exercises the actual admin gate (route middleware), since
// the handlers themselves do not check admin: a normal user is refused, an admin
// group user passes.
func TestRequireAdminGate(t *testing.T) {
	ctl, _, user := ogEnv(t)
	admins := ctl.dep.Config.System.AdminEmailSet()
	adminGroups := ctl.dep.Config.System.AdminGroupSet()
	gate := middleware.RequireAdmin(admins, adminGroups)

	// Normal user: forbidden.
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/admin/users", nil)
	middleware.SetUser(c, user)
	gate(c)
	if !c.IsAborted() {
		t.Fatalf("normal user should be aborted by RequireAdmin")
	}
	if e := decode(t, w); e.Code != serializer.CodeForbidden {
		t.Fatalf("normal user code = %d, want %d", e.Code, serializer.CodeForbidden)
	}

	// Admin-group user: passes.
	admin := makeAdmin(t, ctl, user)
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest("GET", "/admin/users", nil)
	middleware.SetUser(c2, admin)
	gate(c2)
	if c2.IsAborted() {
		t.Fatalf("admin user should pass RequireAdmin (body %s)", w2.Body.String())
	}
}
