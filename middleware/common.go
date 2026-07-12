package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// CORS applies permissive CORS headers suitable for the SPA dev server. In
// production the SPA is served same-origin so this is mostly a no-op.
func CORS(origin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		o := c.GetHeader("Origin")
		if o != "" {
			c.Header("Access-Control-Allow-Origin", o)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Chunk-Index")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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
