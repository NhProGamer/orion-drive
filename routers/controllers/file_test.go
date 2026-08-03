package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// authReq builds a gin.Context carrying an authenticated user, a request with the
// given method/target (query string included) and an optional JSON body.
func authReq(user *model.User, method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	c.Request = httptest.NewRequest(method, target, r)
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	middleware.SetUser(c, user)
	return c, w
}

// envelope is the standard {code,data} API response, with data left raw so each
// test can decode it into the shape it expects.
type envelope struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

// decode unmarshals the recorder body into an envelope, failing the test on error.
func decode(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return e
}

// dtoOf decodes an envelope's data into a fileDTO.
func dtoOf(t *testing.T, e envelope) fileDTO {
	t.Helper()
	var d fileDTO
	if err := json.Unmarshal(e.Data, &d); err != nil {
		t.Fatalf("decode fileDTO %q: %v", e.Data, err)
	}
	return d
}

// dtosOf decodes an envelope's data into a slice of fileDTO.
func dtosOf(t *testing.T, e envelope) []fileDTO {
	t.Helper()
	var d []fileDTO
	if err := json.Unmarshal(e.Data, &d); err != nil {
		t.Fatalf("decode []fileDTO %q: %v", e.Data, err)
	}
	return d
}

func TestCreateFolder(t *testing.T) {
	ctl, mgr, user := ogEnv(t)

	// Root-level folder.
	c, w := authReq(user, "POST", "/file/folder", `{"parent":"root","name":"docs"}`)
	ctl.CreateFolder(c)
	if e := decode(t, w); e.Code != serializer.CodeOK {
		t.Fatalf("CreateFolder code = %d, want 0 (body %s)", e.Code, w.Body.String())
	}
	d := dtoOf(t, decode(t, w))
	if d.Name != "docs" || d.Type != "folder" || d.ParentID != nil {
		t.Fatalf("unexpected folder dto: %+v", d)
	}

	// It must now show up in a listing of the root.
	kids, err := mgr.List(context.Background(), user, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(kids) != 1 || kids[0].Name != "docs" {
		t.Fatalf("root listing = %+v, want one folder 'docs'", kids)
	}

	// Nested folder under the one we just made.
	parentID := d.ID
	c2, w2 := authReq(user, "POST", "/file/folder", `{"parent":"`+utoa(parentID)+`","name":"nested"}`)
	ctl.CreateFolder(c2)
	nd := dtoOf(t, decode(t, w2))
	if nd.ParentID == nil || *nd.ParentID != parentID {
		t.Fatalf("nested folder parent = %v, want %d", nd.ParentID, parentID)
	}

	// A duplicate name at the same level is a conflict.
	c3, w3 := authReq(user, "POST", "/file/folder", `{"parent":"root","name":"docs"}`)
	ctl.CreateFolder(c3)
	if e := decode(t, w3); e.Code != serializer.CodeConflict {
		t.Fatalf("duplicate folder code = %d, want %d", e.Code, serializer.CodeConflict)
	}
}

func TestRename(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	f, err := mgr.CreateFolder(context.Background(), user, nil, "before")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	c, w := authReq(user, "POST", "/file/rename", `{"id":`+utoa(f.ID)+`,"name":"after"}`)
	ctl.Rename(c)
	d := dtoOf(t, decode(t, w))
	if d.Name != "after" {
		t.Fatalf("rename name = %q, want 'after'", d.Name)
	}

	got, _ := ctl.dep.Repo.File.GetByID(context.Background(), user.ID, f.ID)
	if got.Name != "after" {
		t.Fatalf("db name = %q, want 'after'", got.Name)
	}
}

func TestStarToggle(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	f, _ := mgr.WriteFile(context.Background(), user, nil, "s.txt", strings.NewReader("x"), 1)

	body := `{"id":` + utoa(f.ID) + `}`
	c1, w1 := authReq(user, "POST", "/file/star", body)
	ctl.Star(c1)
	if d := dtoOf(t, decode(t, w1)); !d.Starred {
		t.Fatalf("first toggle: starred = false, want true")
	}

	c2, w2 := authReq(user, "POST", "/file/star", body)
	ctl.Star(c2)
	if d := dtoOf(t, decode(t, w2)); d.Starred {
		t.Fatalf("second toggle: starred = true, want false")
	}
}

func TestMove(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	ctx := context.Background()
	dst, _ := mgr.CreateFolder(ctx, user, nil, "dst")
	f, _ := mgr.WriteFile(ctx, user, nil, "m.txt", strings.NewReader("x"), 1)

	c, w := authReq(user, "POST", "/file/move", `{"ids":[`+utoa(f.ID)+`],"parent":"`+utoa(dst.ID)+`"}`)
	ctl.Move(c)
	if e := decode(t, w); e.Code != serializer.CodeOK {
		t.Fatalf("Move code = %d, want 0 (%s)", e.Code, w.Body.String())
	}

	// The file must no longer be at root and must appear under dst.
	root, _ := mgr.List(ctx, user, nil)
	for _, r := range root {
		if r.ID == f.ID {
			t.Fatalf("moved file still at root")
		}
	}
	inDst, _ := mgr.List(ctx, user, &dst.ID)
	if len(inDst) != 1 || inDst[0].ID != f.ID {
		t.Fatalf("dst listing = %+v, want the moved file", inDst)
	}
}

func TestTrashAndRestore(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	ctx := context.Background()
	f, _ := mgr.WriteFile(ctx, user, nil, "t.txt", strings.NewReader("x"), 1)

	// Trash it: gone from the drive, present in the recycle bin.
	c, w := authReq(user, "POST", "/file/trash", `{"ids":[`+utoa(f.ID)+`]}`)
	ctl.Trash(c)
	if e := decode(t, w); e.Code != serializer.CodeOK {
		t.Fatalf("Trash code = %d (%s)", e.Code, w.Body.String())
	}
	if root, _ := mgr.List(ctx, user, nil); len(root) != 0 {
		t.Fatalf("trashed file still listed at root: %+v", root)
	}
	if trash, _ := mgr.ListTrashed(ctx, user); len(trash) != 1 || trash[0].ID != f.ID {
		t.Fatalf("trash = %+v, want the one file", trash)
	}

	// Restore it: back on the drive, gone from the bin.
	c2, w2 := authReq(user, "POST", "/file/restore", `{"ids":[`+utoa(f.ID)+`]}`)
	ctl.Restore(c2)
	if e := decode(t, w2); e.Code != serializer.CodeOK {
		t.Fatalf("Restore code = %d (%s)", e.Code, w2.Body.String())
	}
	if root, _ := mgr.List(ctx, user, nil); len(root) != 1 || root[0].ID != f.ID {
		t.Fatalf("restored file not at root: %+v", root)
	}
	if trash, _ := mgr.ListTrashed(ctx, user); len(trash) != 0 {
		t.Fatalf("bin not empty after restore: %+v", trash)
	}
}

func TestAncestors(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	ctx := context.Background()
	a, _ := mgr.CreateFolder(ctx, user, nil, "a")
	b, _ := mgr.CreateFolder(ctx, user, &a.ID, "b")
	cc, _ := mgr.CreateFolder(ctx, user, &b.ID, "c")
	file, _ := mgr.WriteFile(ctx, user, &cc.ID, "deep.txt", strings.NewReader("x"), 1)

	crumbNames := func(id uint) []string {
		c, w := authReq(user, "GET", "/file/ancestors/"+utoa(id), "")
		c.Params = gin.Params{{Key: "id", Value: utoa(id)}}
		ctl.Ancestors(c)
		e := decode(t, w)
		if e.Code != serializer.CodeOK {
			t.Fatalf("Ancestors(%d) code = %d (%s)", id, e.Code, w.Body.String())
		}
		var crumbs []struct{ Name string }
		if err := json.Unmarshal(e.Data, &crumbs); err != nil {
			t.Fatalf("decode crumbs: %v", err)
		}
		names := make([]string, len(crumbs))
		for i := range crumbs {
			names[i] = crumbs[i].Name
		}
		return names
	}

	// A folder's trail is root-first and includes itself.
	if got := crumbNames(cc.ID); strings.Join(got, "/") != "a/b/c" {
		t.Fatalf("folder ancestors = %v, want [a b c]", got)
	}
	// A file's trail is its containing folder chain (excludes the file).
	if got := crumbNames(file.ID); strings.Join(got, "/") != "a/b/c" {
		t.Fatalf("file ancestors = %v, want [a b c]", got)
	}

	// Another owner may not read the trail — it is a not-found.
	other := &model.User{Email: "x@example.com", Subject: "x", GroupID: 1}
	if err := ctl.dep.Repo.User.Create(ctx, other); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	c, w := authReq(other, "GET", "/file/ancestors/"+utoa(cc.ID), "")
	c.Params = gin.Params{{Key: "id", Value: utoa(cc.ID)}}
	ctl.Ancestors(c)
	if e := decode(t, w); e.Code != serializer.CodeNotFound {
		t.Fatalf("cross-owner ancestors code = %d, want %d", e.Code, serializer.CodeNotFound)
	}
}

func TestListFilesSearchFilters(t *testing.T) {
	ctl, _, user := ogEnv(t)
	mgr := ctl.dep.Files
	ctx := context.Background()
	// Seed a small mixed corpus.
	folder, _ := mgr.CreateFolder(ctx, user, nil, "reportfolder")
	small, _ := mgr.WriteFile(ctx, user, nil, "report-small.txt", strings.NewReader("aa"), 2)
	big, _ := mgr.WriteFile(ctx, user, nil, "report-big.bin", bytes.NewReader(make([]byte, 5000)), 5000)
	img, _ := mgr.WriteFile(ctx, user, nil, "picture.png", bytes.NewReader(ogPNG(t)), int64(len(ogPNG(t))))
	other, _ := mgr.WriteFile(ctx, user, nil, "unrelated.txt", strings.NewReader("z"), 1)
	_ = other

	// Collect the ids returned for a given query string.
	ids := func(query string) map[uint]bool {
		c, w := authReq(user, "GET", "/file?"+query, "")
		ctl.ListFiles(c)
		e := decode(t, w)
		if e.Code != serializer.CodeOK {
			t.Fatalf("ListFiles(%q) code = %d (%s)", query, e.Code, w.Body.String())
		}
		set := map[uint]bool{}
		for _, d := range dtosOf(t, e) {
			set[d.ID] = true
		}
		return set
	}

	// q= substring matches every "report*" item (2 files + 1 folder), not the others.
	q := ids("q=report")
	if !q[small.ID] || !q[big.ID] || !q[folder.ID] {
		t.Fatalf("q=report missing expected hits: %v", q)
	}
	if q[img.ID] || q[other.ID] {
		t.Fatalf("q=report leaked non-matching files: %v", q)
	}

	// type=folder narrows to folders only.
	tf := ids("q=report&type=folder")
	if !tf[folder.ID] || tf[small.ID] || tf[big.ID] {
		t.Fatalf("type=folder wrong set: %v", tf)
	}

	// kind=images narrows to image files only.
	ki := ids("kind=images")
	if !ki[img.ID] || ki[small.ID] || ki[folder.ID] {
		t.Fatalf("kind=images wrong set: %v", ki)
	}

	// min_size excludes the tiny files, keeping the 5000-byte blob.
	mn := ids("min_size=1000")
	if !mn[big.ID] || mn[small.ID] {
		t.Fatalf("min_size=1000 wrong set: %v", mn)
	}

	// max_size keeps only small items, dropping the big blob.
	mx := ids("q=report&max_size=100")
	if !mx[small.ID] || mx[big.ID] {
		t.Fatalf("max_size=100 wrong set: %v", mx)
	}

	// before= in the past excludes everything (all seeded just now).
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if got := ids("q=report&before=" + past); len(got) != 0 {
		t.Fatalf("before=past should match nothing, got %v", got)
	}
	// after= in the past includes the recent items.
	if got := ids("q=report&after=" + past); !got[small.ID] {
		t.Fatalf("after=past should include recent files, got %v", got)
	}
}

func TestEnsureFolderPath(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	ctx := context.Background()

	c, w := authReq(user, "POST", "/file/ensure-path", `{"parent":"root","path":"x/y/z"}`)
	ctl.EnsureFolderPath(c)
	e := decode(t, w)
	if e.Code != serializer.CodeOK {
		t.Fatalf("EnsureFolderPath code = %d (%s)", e.Code, w.Body.String())
	}
	leaf := dtoOf(t, e)
	if leaf.Name != "z" {
		t.Fatalf("leaf name = %q, want 'z'", leaf.Name)
	}

	// The whole chain must now exist: root->x->y->z.
	x, err := ctl.dep.Repo.File.FindChildByName(ctx, user.ID, nil, "x")
	if err != nil {
		t.Fatalf("x not created: %v", err)
	}
	if _, err := ctl.dep.Repo.File.FindChildByName(ctx, user.ID, &x.ID, "y"); err != nil {
		t.Fatalf("y not created: %v", err)
	}

	// Idempotent: a second call returns the same leaf, not a duplicate.
	c2, w2 := authReq(user, "POST", "/file/ensure-path", `{"parent":"root","path":"x/y/z"}`)
	ctl.EnsureFolderPath(c2)
	leaf2 := dtoOf(t, decode(t, w2))
	if leaf2.ID != leaf.ID {
		t.Fatalf("second ensure gave id %d, want %d", leaf2.ID, leaf.ID)
	}
	// Still exactly one "x" at root.
	roots, _ := mgr.List(ctx, user, nil)
	count := 0
	for _, r := range roots {
		if r.Name == "x" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one 'x' folder, found %d", count)
	}
}

// utoa is a tiny uint->string helper for building request bodies/paths.
func utoa(u uint) string { return strconv.FormatUint(uint64(u), 10) }
