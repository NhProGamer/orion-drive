package middleware

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestLoggingLevelByStatus proves the request logger elevates its level by the
// response status: 2xx→INFO, 4xx→WARN, 5xx→ERROR.
func TestLoggingLevelByStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	levelFor := func(status int) string {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		w := httptest.NewRecorder()
		_, e := gin.CreateTestContext(w)
		e.Use(Logging(logger))
		e.GET("/x", func(c *gin.Context) { c.Status(status) })
		e.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
		for _, lv := range []string{"ERROR", "WARN", "INFO"} {
			if strings.Contains(buf.String(), "level="+lv) {
				return lv
			}
		}
		return ""
	}

	cases := map[int]string{200: "INFO", 404: "WARN", 500: "ERROR"}
	for status, want := range cases {
		if got := levelFor(status); got != want {
			t.Errorf("status %d logged at %q, want %q", status, got, want)
		}
	}
}
