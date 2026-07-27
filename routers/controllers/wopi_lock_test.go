package controllers

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// wopiCall drives a WOPI lock operation (POST /wopi/files/:id) with the given
// override/lock headers through a real gin engine (so deferred status writes are
// flushed) and returns the recorder.
func wopiCall(ctl *Controller, id uint, tok, override, lock, oldLock string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, e := gin.CreateTestContext(w)
	e.POST("/wopi/files/:id", ctl.WopiLock)
	sid := strconv.FormatUint(uint64(id), 10)
	req := httptest.NewRequest("POST", "/wopi/files/"+sid+"?access_token="+tok, nil)
	if override != "" {
		req.Header.Set("X-WOPI-Override", override)
	}
	if lock != "" {
		req.Header.Set("X-WOPI-Lock", lock)
	}
	if oldLock != "" {
		req.Header.Set("X-WOPI-OldLock", oldLock)
	}
	e.ServeHTTP(w, req)
	return w
}

// TestWopiLockLifecycle exercises the LOCK/GET_LOCK/REFRESH/UNLOCK state machine
// and its conflict responses.
func TestWopiLockLifecycle(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	f, err := mgr.WriteFile(context.Background(), user, nil, "doc.docx", bytes.NewReader([]byte("x")), 1)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	tok, err := ctl.dep.WOPI.Sign(f.ID, user.ID, true, "", time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Take the lock.
	if w := wopiCall(ctl, f.ID, tok, "LOCK", "L1", ""); w.Code != 200 {
		t.Fatalf("LOCK L1: got %d", w.Code)
	}
	// A different lock conflicts and reports the current one.
	if w := wopiCall(ctl, f.ID, tok, "LOCK", "L2", ""); w.Code != 409 || w.Header().Get("X-WOPI-Lock") != "L1" {
		t.Fatalf("LOCK L2: got %d lock=%q", w.Code, w.Header().Get("X-WOPI-Lock"))
	}
	// GET_LOCK reports the holder.
	if w := wopiCall(ctl, f.ID, tok, "GET_LOCK", "", ""); w.Code != 200 || w.Header().Get("X-WOPI-Lock") != "L1" {
		t.Fatalf("GET_LOCK: got %d lock=%q", w.Code, w.Header().Get("X-WOPI-Lock"))
	}
	// Refresh with the wrong lock conflicts; with the right one succeeds.
	if w := wopiCall(ctl, f.ID, tok, "REFRESH_LOCK", "L2", ""); w.Code != 409 {
		t.Fatalf("REFRESH wrong: got %d", w.Code)
	}
	if w := wopiCall(ctl, f.ID, tok, "REFRESH_LOCK", "L1", ""); w.Code != 200 {
		t.Fatalf("REFRESH right: got %d", w.Code)
	}
	// Unlock with the wrong lock conflicts; the right one releases it.
	if w := wopiCall(ctl, f.ID, tok, "UNLOCK", "L2", ""); w.Code != 409 {
		t.Fatalf("UNLOCK wrong: got %d", w.Code)
	}
	if w := wopiCall(ctl, f.ID, tok, "UNLOCK", "L1", ""); w.Code != 200 {
		t.Fatalf("UNLOCK right: got %d", w.Code)
	}
	// Now unlocked.
	if w := wopiCall(ctl, f.ID, tok, "GET_LOCK", "", ""); w.Code != 200 || w.Header().Get("X-WOPI-Lock") != "" {
		t.Fatalf("GET_LOCK after unlock: got %d lock=%q", w.Code, w.Header().Get("X-WOPI-Lock"))
	}
}

// TestWopiLockReadOnly proves a view-only token cannot take a lock.
func TestWopiLockReadOnly(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	f, _ := mgr.WriteFile(context.Background(), user, nil, "doc.docx", bytes.NewReader([]byte("x")), 1)
	tok, _ := ctl.dep.WOPI.Sign(f.ID, user.ID, false, "", time.Hour) // canWrite=false
	if w := wopiCall(ctl, f.ID, tok, "LOCK", "L1", ""); w.Code != 403 {
		t.Fatalf("read-only LOCK should be 403, got %d", w.Code)
	}
	// But GET_LOCK is allowed (read-only).
	if w := wopiCall(ctl, f.ID, tok, "GET_LOCK", "", ""); w.Code != 200 {
		t.Fatalf("read-only GET_LOCK should be 200, got %d", w.Code)
	}
}

// TestWopiPutFileRespectsLock proves a save carrying a different lock than the
// held one is rejected 409, while the correct lock (or none) is accepted.
func TestWopiPutFileRespectsLock(t *testing.T) {
	ctl, mgr, user := ogEnv(t)
	f, _ := mgr.WriteFile(context.Background(), user, nil, "doc.docx", bytes.NewReader([]byte("orig")), 4)
	tok, _ := ctl.dep.WOPI.Sign(f.ID, user.ID, true, "", time.Hour)

	// Editor A takes the lock.
	if w := wopiCall(ctl, f.ID, tok, "LOCK", "A", ""); w.Code != 200 {
		t.Fatalf("LOCK A: %d", w.Code)
	}

	put := func(lock, body string) int {
		w := httptest.NewRecorder()
		_, e := gin.CreateTestContext(w)
		e.POST("/wopi/files/:id/contents", ctl.WopiPutFile)
		sid := strconv.FormatUint(uint64(f.ID), 10)
		req := httptest.NewRequest("POST", "/wopi/files/"+sid+"/contents?access_token="+tok, bytes.NewReader([]byte(body)))
		if lock != "" {
			req.Header.Set("X-WOPI-Lock", lock)
		}
		e.ServeHTTP(w, req)
		return w.Code
	}

	// A save under a different lock is rejected.
	if code := put("B", "hijack"); code != 409 {
		t.Fatalf("PutFile with wrong lock should be 409, got %d", code)
	}
	// The lock holder saves fine.
	if code := put("A", "legit edit"); code != 200 {
		t.Fatalf("PutFile with held lock should be 200, got %d", code)
	}
}
