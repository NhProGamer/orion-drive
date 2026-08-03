package controllers

import (
	"encoding/json"
	"testing"

	"github.com/NhProGamer/orion-drive/pkg/auth"
	"github.com/NhProGamer/orion-drive/pkg/serializer"
)

func TestMeAuthenticated(t *testing.T) {
	ctl, _, user := ogEnv(t)
	// Me touches ctl.dep.Auth.Enabled(); ogEnv leaves Auth nil, so wire an empty
	// (disabled) authenticator.
	ctl.dep.Auth = &auth.Authenticator{}

	c, w := authReq(user, "GET", "/user/me", "")
	ctl.Me(c)
	e := decode(t, w)
	if e.Code != serializer.CodeOK {
		t.Fatalf("Me code = %d (%s)", e.Code, w.Body.String())
	}
	var body struct {
		ID          uint   `json:"id"`
		Email       string `json:"email"`
		CanShare    bool   `json:"can_share"`
		Admin       bool   `json:"admin"`
		OIDCEnabled bool   `json:"oidc_enabled"`
	}
	if err := json.Unmarshal(e.Data, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ID != user.ID || body.Email != user.Email {
		t.Fatalf("Me identity = %+v, want id %d / %s", body, user.ID, user.Email)
	}
	if !body.CanShare {
		t.Fatalf("default group should allow sharing")
	}
	if body.Admin {
		t.Fatalf("seeded user should not be admin")
	}
	if body.OIDCEnabled {
		t.Fatalf("disabled authenticator should report oidc_enabled=false")
	}
}

func TestMeUnauthenticated(t *testing.T) {
	ctl, _, _ := ogEnv(t)
	// No user in context.
	c, w := authReq(nil, "GET", "/user/me", "")
	ctl.Me(c)
	if e := decode(t, w); e.Code != serializer.CodeUnauthorized {
		t.Fatalf("unauth Me code = %d, want %d (%s)", e.Code, serializer.CodeUnauthorized, w.Body.String())
	}
}

func TestLogout(t *testing.T) {
	ctl, _, user := ogEnv(t)
	ctl.dep.Auth = &auth.Authenticator{}

	c, w := authReq(user, "POST", "/user/logout", "")
	ctl.Logout(c)
	e := decode(t, w)
	if e.Code != serializer.CodeOK {
		t.Fatalf("Logout code = %d (%s)", e.Code, w.Body.String())
	}
	var body struct {
		LogoutURL string `json:"logout_url"`
	}
	if err := json.Unmarshal(e.Data, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// A disabled provider yields no RP-initiated logout URL.
	if body.LogoutURL != "" {
		t.Fatalf("logout_url = %q, want empty for disabled provider", body.LogoutURL)
	}
	// The session cookie must be cleared (Max-Age<=0).
	var cleared bool
	for _, ck := range w.Result().Cookies() {
		if ck.Name != "" && ck.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("expected a cleared cookie in the response, got %v", w.Result().Cookies())
	}
}
