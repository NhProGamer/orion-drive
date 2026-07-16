package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// ErrOIDCDisabled is returned when a login is attempted but no provider is configured.
var ErrOIDCDisabled = errors.New("OIDC is not configured")

// Claims holds the identity fields OrionDrive consumes from an ID token.
type Claims struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
	// Groups are the SSO groups/roles from the configured claim (used to map the
	// user onto an OrionDrive group).
	Groups []string `json:"-"`
	// IDToken is the raw ID token, kept for use as id_token_hint on logout.
	IDToken string `json:"-"`
}

// Authenticator wraps OIDC discovery, the OAuth2 config and the token verifier,
// and manages short-lived login states in the cache.
type Authenticator struct {
	oauth       *oauth2.Config
	verifier    *oidc.IDTokenVerifier
	cache       cache.Store
	enabled     bool
	groupsClaim string
	endSession  string
}

// NewAuthenticator performs provider discovery. If OIDC is not configured (no
// client id) it returns a disabled authenticator so the server can still boot.
func NewAuthenticator(ctx context.Context, cfg *conf.Config, c cache.Store) (*Authenticator, error) {
	if cfg.OIDC.ClientID == "" || cfg.OIDC.Issuer == "" {
		return &Authenticator{cache: c, enabled: false}, nil
	}
	provider, err := oidc.NewProvider(ctx, cfg.OIDC.Issuer)
	if err != nil {
		return nil, err
	}
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.OIDC.RedirectURI,
		Scopes:       cfg.OIDC.ScopeList(),
	}
	groupsClaim := cfg.OIDC.GroupsClaim
	if groupsClaim == "" {
		groupsClaim = "groups"
	}
	// The end_session_endpoint is optional in discovery; when present it enables
	// RP-initiated logout so signing out also ends the SSO session.
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = provider.Claims(&meta)
	return &Authenticator{
		oauth:       oauthCfg,
		verifier:    provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID}),
		cache:       c,
		enabled:     true,
		groupsClaim: groupsClaim,
		endSession:  meta.EndSession,
	}, nil
}

// LogoutURL builds the provider's RP-initiated logout URL, sending the browser
// back to postLogoutRedirect once the SSO session ends. It returns "" when the
// provider advertises no end_session_endpoint, in which case the caller should
// just clear the local session.
func (a *Authenticator) LogoutURL(idToken, postLogoutRedirect string) string {
	if !a.enabled || a.endSession == "" {
		return ""
	}
	q := url.Values{}
	if idToken != "" {
		q.Set("id_token_hint", idToken)
	}
	if a.oauth != nil && a.oauth.ClientID != "" {
		q.Set("client_id", a.oauth.ClientID)
	}
	if postLogoutRedirect != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirect)
	}
	sep := "?"
	if strings.Contains(a.endSession, "?") {
		// The endpoint may already carry a query string.
		sep = "&"
	}
	return a.endSession + sep + q.Encode()
}

// Enabled reports whether a provider is configured.
func (a *Authenticator) Enabled() bool { return a.enabled }

// AuthCodeURL creates a login redirect URL and stores its anti-CSRF state.
func (a *Authenticator) AuthCodeURL() (string, error) {
	if !a.enabled {
		return "", ErrOIDCDisabled
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	state := hex.EncodeToString(buf)
	if err := a.cache.Set("oidc_state:"+state, []byte{1}, 10*time.Minute); err != nil {
		return "", err
	}
	return a.oauth.AuthCodeURL(state), nil
}

// Exchange validates the returned state, exchanges the code and verifies the
// ID token, returning its claims.
func (a *Authenticator) Exchange(ctx context.Context, state, code string) (*Claims, error) {
	if !a.enabled {
		return nil, ErrOIDCDisabled
	}
	if _, ok := a.cache.Get("oidc_state:" + state); !ok {
		return nil, errors.New("invalid or expired login state")
	}
	_ = a.cache.Delete("oidc_state:" + state)

	token, err := a.oauth.Exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("no id_token in response")
	}
	idToken, err := a.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, err
	}
	var c Claims
	if err := idToken.Claims(&c); err != nil {
		return nil, err
	}
	c.IDToken = rawID
	if c.Subject == "" {
		c.Subject = idToken.Subject
	}
	// Extract SSO groups from the configured claim (name varies by provider:
	// "groups", "roles", ...). Accept an array of strings or a single string.
	var raw map[string]any
	if err := idToken.Claims(&raw); err == nil {
		c.Groups = extractStrings(raw[a.groupsClaim])
	}
	return &c, nil
}

// extractStrings coerces a JSON claim value into a string slice.
func extractStrings(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	case string:
		if t != "" {
			return []string{t}
		}
	}
	return nil
}
