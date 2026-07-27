package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders sets defensive response headers on every request:
//   - X-Content-Type-Options: nosniff — browsers must not MIME-sniff responses.
//   - X-Frame-Options: SAMEORIGIN — our pages may only be framed same-origin
//     (clickjacking defence; the Office editor frames the document server, not us).
//   - Referrer-Policy: strict-origin-when-cross-origin — don't leak full URLs
//     (share tokens) to third parties.
//
// The Content-Security-Policy is operator-configured (System.CSP) and only sent
// when non-empty: a correct policy must name the deployment's document server
// (WOPI iframe) and the launcher runs an inline script, so a blanket default
// would break Office editing.
func SecurityHeaders(csp string) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if csp != "" {
			h.Set("Content-Security-Policy", csp)
		}
		c.Next()
	}
}
