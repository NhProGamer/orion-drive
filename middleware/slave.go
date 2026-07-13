package middleware

import (
	"net/http"

	"github.com/NhProGamer/orion-drive/pkg/slaveauth"
	"github.com/gin-gonic/gin"
)

// SlaveAuth rejects slave storage requests that are not signed with the shared
// secret. It verifies the method, path and query against the signature.
func SlaveAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if secret == "" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if err := slaveauth.Verify(secret, c.Request.Method, c.Request.URL.Path, c.Request.URL.Query()); err != nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}
