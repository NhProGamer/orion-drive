package middleware

import (
	"testing"

	"github.com/NhProGamer/orion-drive/model"
)

func adminGroup(admin bool) *model.Group {
	return &model.Group{Permissions: model.MustJSON(model.GroupPermissions{Admin: &admin})}
}

func TestIsAdmin(t *testing.T) {
	emails := map[string]bool{"boss@x.io": true}
	ssoAdmins := map[string]bool{"sysops": true}

	cases := []struct {
		name string
		u    *model.User
		want bool
	}{
		{"nil user", nil, false},
		{"by email", &model.User{Email: "BOSS@x.io"}, true}, // case-insensitive
		{"by sso admin group", &model.User{Email: "u@x.io", SSOGroups: "team, sysops"}, true},
		{"by group permission", &model.User{Email: "u@x.io", Group: adminGroup(true)}, true},
		{"plain user", &model.User{Email: "u@x.io", SSOGroups: "team", Group: adminGroup(false)}, false},
		{"no group, no sso", &model.User{Email: "u@x.io"}, false},
	}
	for _, c := range cases {
		if got := IsAdmin(c.u, emails, ssoAdmins); got != c.want {
			t.Errorf("%s: IsAdmin = %v, want %v", c.name, got, c.want)
		}
	}
}
