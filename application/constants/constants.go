// Package constants holds compile-time application constants.
package constants

// Version, Commit and Date describe the build. They default to a development
// value and are overridden at release time via -ldflags -X (see .goreleaser.yaml).
var (
	Version = "0.6.0-dev"
	Commit  = "none"
	Date    = "unknown"
)

// APIPrefix is the base path for all JSON API routes.
const APIPrefix = "/api/v1"

// SessionCookieName is the name of the session cookie.
const SessionCookieName = "orion_session"

// IDTokenCookieName holds the raw OIDC ID token, used as id_token_hint for
// RP-initiated logout at the provider.
const IDTokenCookieName = "orion_id_token"
