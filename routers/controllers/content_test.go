package controllers

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestServeContentInlineSafety proves serveContent serves a viewer-safe type
// inline with nosniff, but forces script-capable / unknown types to an
// attachment download — the shared hardening for authed, direct-link and share
// content.
func TestServeContentInlineSafety(t *testing.T) {
	gin.SetMode(gin.TestMode)

	call := func(name string, inline bool) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/x", nil)
		serveContent(c, name, time.Unix(0, 0), strings.NewReader("body"), inline)
		return w
	}

	// Safe type, inline requested → inline + exact type + nosniff.
	w := call("photo.png", true)
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "inline;") {
		t.Fatalf("png inline: disposition = %q", cd)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("png inline: content-type = %q", ct)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("png inline: missing nosniff")
	}

	// Script-capable types are forced to attachment even when inline is requested.
	for _, name := range []string{"page.html", "vector.svg", "app.js"} {
		w := call(name, true)
		if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
			t.Fatalf("%s: expected attachment, got %q", name, cd)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
			t.Fatalf("%s: expected octet-stream, got %q", name, ct)
		}
	}

	// inline=false always downloads, even for a safe type.
	if cd := call("photo.png", false).Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Fatalf("png download: disposition = %q", cd)
	}
}
