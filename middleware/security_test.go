package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	run := func(csp string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		_, e := gin.CreateTestContext(w)
		e.Use(SecurityHeaders(csp))
		e.GET("/x", func(c *gin.Context) { c.Status(200) })
		e.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
		return w
	}

	w := run("")
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("x-frame-options = %q", got)
	}
	if w.Header().Get("Referrer-Policy") == "" {
		t.Fatalf("referrer-policy missing")
	}
	if got := w.Header().Get("Content-Security-Policy"); got != "" {
		t.Fatalf("empty CSP config must not send the header, got %q", got)
	}

	w = run("default-src 'self'")
	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Fatalf("configured CSP = %q", got)
	}
}
