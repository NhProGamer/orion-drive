package middleware

import (
	"fmt"
	"strconv"
	"time"

	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// RateLimit throttles requests per client IP using a fixed one-minute window in
// the shared cache. It guards public endpoints (share links) against password
// brute-forcing and anonymous-write abuse. perMin <= 0 disables it. scope keys
// separate limiters so they don't share a budget.
//
// The window counter is best-effort: a race between concurrent requests may
// under-count slightly, which is fine for throttling. Behind a reverse proxy the
// client IP is whatever Gin's trusted-proxy configuration resolves.
func RateLimit(store cache.Store, perMin int, scope string) gin.HandlerFunc {
	if perMin <= 0 || store == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		window := time.Now().Unix() / 60
		key := fmt.Sprintf("rl:%s:%s:%d", scope, c.ClientIP(), window)
		n := 0
		if b, ok := store.Get(key); ok {
			n, _ = strconv.Atoi(string(b))
		}
		if n >= perMin {
			r := serializer.Err(serializer.CodeTooManyReqs, "too many requests, slow down")
			c.AbortWithStatusJSON(r.HTTPStatus(), r)
			return
		}
		_ = store.Set(key, []byte(strconv.Itoa(n+1)), 70*time.Second)
		c.Next()
	}
}
