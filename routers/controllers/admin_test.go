package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/crontab"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
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

func TestAdminTasks(t *testing.T) {
	ctl, _, user := ogEnv(t)
	started := make(chan struct{})
	ctl.dep.Cron.Add("demo", time.Hour, func(ctx context.Context, r crontab.Report) (int64, error) {
		r(3, 10)
		close(started)
		<-ctx.Done()
		return 3, ctx.Err()
	})
	call := func(h gin.HandlerFunc, id string) envelope {
		c, w := authReq(user, "POST", "/admin/tasks/"+id, "")
		c.Params = gin.Params{{Key: "id", Value: id}}
		h(c)
		return decode(t, w)
	}
	list := func() []crontab.State {
		c, w := authReq(user, "GET", "/admin/tasks", "")
		ctl.AdminListTasks(c)
		var out []crontab.State
		if err := json.Unmarshal(decode(t, w).Data, &out); err != nil || len(out) != 1 {
			t.Fatalf("tasks = %s", w.Body.String())
		}
		return out
	}

	if e := call(ctl.AdminRunTask, "nope"); e.Code != serializer.CodeNotFound {
		t.Fatalf("unknown task code = %d", e.Code)
	}
	if e := call(ctl.AdminRunTask, "demo"); e.Code != serializer.CodeOK {
		t.Fatalf("run code = %d", e.Code)
	}
	<-started
	if e := call(ctl.AdminRunTask, "demo"); e.Code != serializer.CodeConflict {
		t.Fatalf("second run code = %d, want conflict", e.Code)
	}
	if st := list()[0]; st.Status != crontab.StatusRunning || st.Done != 3 || st.Total != 10 {
		t.Fatalf("running state = %+v", st)
	}
	if e := call(ctl.AdminStopTask, "demo"); e.Code != serializer.CodeOK {
		t.Fatalf("stop code = %d", e.Code)
	}
	for deadline := time.Now().Add(5 * time.Second); list()[0].Status == crontab.StatusRunning; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("task did not stop")
		}
	}
	if st := list()[0]; st.Status != crontab.StatusStopped || len(st.History) != 1 {
		t.Fatalf("stopped state = %+v", st)
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

// slavePolicy creates a second local policy and returns it with a helper that
// points the user's group at a policy (reloading the user).
func slavePolicy(t *testing.T, ctl *Controller, user **model.User) (*model.StoragePolicy, func(uint)) {
	t.Helper()
	ctx := context.Background()
	repo := ctl.dep.Repo
	p := &model.StoragePolicy{Name: "slave", Type: model.PolicyTypeLocal, BasePath: filepath.Join(t.TempDir(), "s2")}
	if err := repo.Policy.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	return p, func(id uint) {
		t.Helper()
		if err := repo.Group.SetStoragePolicy(ctx, (*user).GroupID, id); err != nil {
			t.Fatal(err)
		}
		*user, _ = repo.User.GetByID(ctx, (*user).ID)
	}
}

// TestAdminDeletePolicyPurgesItsFiles: deleting a policy that still holds data
// moves its groups to the default policy, deletes files whose current version
// lives there, and only strips older versions from files living elsewhere.
func TestAdminDeletePolicyPurgesItsFiles(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	ctx := context.Background()
	repo := ctl.dep.Repo
	gone, usePolicy := slavePolicy(t, ctl, &user)

	usePolicy(gone.ID)
	a, err := mgr.WriteFile(ctx, user, nil, "a.txt", strings.NewReader("aaaa"), 4)
	if err != nil {
		t.Fatal(err)
	}
	b, err := mgr.WriteFile(ctx, user, nil, "b.txt", strings.NewReader("old"), 3)
	if err != nil {
		t.Fatal(err)
	}
	usePolicy(filemanager.DefaultPolicyID)
	if _, err := mgr.SaveVersion(ctx, user, b.ID, []byte("newer")); err != nil {
		t.Fatal(err)
	}
	usePolicy(gone.ID)

	params := gin.Params{{Key: "id", Value: strconv.Itoa(int(gone.ID))}}
	cu, wu := authReq(user, "GET", "/admin/policies/x/usage", "")
	cu.Params = params
	ctl.AdminPolicyUsage(cu)
	var usage struct{ Groups, Files int }
	if e := decode(t, wu); e.Code != serializer.CodeOK || json.Unmarshal(e.Data, &usage) != nil || usage.Groups != 1 || usage.Files != 1 {
		t.Fatalf("usage = %+v (%s), want 1 group / 1 file", usage, wu.Body.String())
	}

	cd, wd := authReq(user, "DELETE", "/admin/policies/x", "")
	cd.Params = params
	ctl.AdminDeletePolicy(cd)
	if e := decode(t, wd); e.Code != serializer.CodeOK {
		t.Fatalf("delete code = %d (%s)", e.Code, wd.Body.String())
	}
	// The purge runs in the background; the policy row is the last thing to go.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := repo.Policy.GetByID(ctx, gone.ID); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("policy still exists")
		}
	}

	if _, err := repo.File.GetByIDUnscoped(ctx, user.ID, a.ID); err == nil {
		t.Fatal("file stored on the deleted policy survived")
	}
	if vs, _ := repo.Entity.ListVersions(ctx, b.ID); len(vs) != 1 || vs[0].StoragePolicyID != filemanager.DefaultPolicyID {
		t.Fatalf("b versions = %+v, want only the one on the default policy", vs)
	}
	if left, _ := repo.Entity.ListByPolicy(ctx, gone.ID); len(left) != 0 {
		t.Fatalf("%d entities left on the deleted policy", len(left))
	}
	user, _ = repo.User.GetByID(ctx, user.ID)
	if user.Group.StoragePolicyID != filemanager.DefaultPolicyID {
		t.Fatalf("group policy = %d, want default", user.Group.StoragePolicyID)
	}
	if user.StorageUsed != 5 {
		t.Fatalf("storage used = %d, want 5 (only \"newer\")", user.StorageUsed)
	}

	// The default policy itself is protected.
	c1, w1 := authReq(user, "DELETE", "/admin/policies/1", "")
	c1.Params = gin.Params{{Key: "id", Value: "1"}}
	ctl.AdminDeletePolicy(c1)
	if e := decode(t, w1); e.Code == serializer.CodeOK {
		t.Fatal("default policy was deleted")
	}
}

// TestResumePolicyDeletion: a deletion cut short mid-purge (entities already
// gone, file rows left) is finished by the scheduled resume, including files
// whose owner account no longer exists; groups cannot be pointed at it meanwhile.
func TestResumePolicyDeletion(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	ctx := context.Background()
	repo := ctl.dep.Repo
	gone, usePolicy := slavePolicy(t, ctl, &user)
	usePolicy(gone.ID)

	half, err := mgr.WriteFile(ctx, user, nil, "half.txt", strings.NewReader("x"), 1)
	if err != nil {
		t.Fatal(err)
	}
	ghost := &model.User{Email: "ghost@example.com", Subject: "ghost", GroupID: user.GroupID}
	if err := repo.User.Create(ctx, ghost); err != nil {
		t.Fatal(err)
	}
	ghost, _ = repo.User.GetByID(ctx, ghost.ID)
	orphan, err := mgr.WriteFile(ctx, ghost, nil, "orphan.txt", strings.NewReader("y"), 1)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the crash: deletion started, half.txt lost its entities but not its
	// row, and the ghost account was removed.
	if err := repo.Policy.MarkDeleting(ctx, gone); err != nil {
		t.Fatal(err)
	}
	if err := ctl.dep.DB.Exec("DELETE FROM entities WHERE file_id = ?", half.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := ctl.dep.DB.Exec("DELETE FROM users WHERE id = ?", ghost.ID).Error; err != nil {
		t.Fatal(err)
	}

	// A group cannot be moved onto a policy being deleted.
	body := fmt.Sprintf(`{"name":"g","storage_policy_id":%d}`, gone.ID)
	cg, wg := authReq(user, "POST", "/admin/groups", body)
	ctl.AdminCreateGroup(cg)
	if e := decode(t, wg); e.Code == serializer.CodeOK {
		t.Fatal("group created on a deleting policy")
	}

	if n, err := mgr.ResumePolicyDeletions(ctx, nil); err != nil || n != 1 {
		t.Fatalf("resume = %d, %v; want 1 policy", n, err)
	}
	if _, err := repo.Policy.GetByID(ctx, gone.ID); err == nil {
		t.Fatal("policy still exists")
	}
	if _, err := repo.File.GetByIDUnscoped(ctx, user.ID, half.ID); err == nil {
		t.Fatal("half-purged file row survived")
	}
	if _, err := repo.File.GetByIDUnscoped(ctx, ghost.ID, orphan.ID); err == nil {
		t.Fatal("file of a deleted owner survived")
	}
}
