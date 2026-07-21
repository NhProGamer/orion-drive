package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// CORS grants credentialed cross-origin access ONLY to the configured site
// origin. Reflecting an arbitrary Origin (as this did before) while also setting
// Allow-Credentials lets any website make authenticated cross-origin calls to
// the API and read the responses. In production the SPA is served same-origin,
// so only that one origin ever needs to be allowed.
func CORS(siteURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if o := c.GetHeader("Origin"); o != "" && o == siteURL {
			c.Header("Access-Control-Allow-Origin", o)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Chunk-Index")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Vary", "Origin")
		}
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

// Logging emits one structured line per request.
func Logging(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"dur", time.Since(start).String(),
		)
	}
}
