package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
}

// Authenticator wraps OIDC discovery, the OAuth2 config and the token verifier,
// and manages short-lived login states in the cache.
type Authenticator struct {
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
	cache    cache.Store
	enabled  bool
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
	return &Authenticator{
		oauth:    oauthCfg,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID}),
		cache:    c,
		enabled:  true,
	}, nil
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
	if c.Subject == "" {
		c.Subject = idToken.Subject
	}
	return &c, nil
}
