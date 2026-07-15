package model

import "strings"

// Group is a permission and quota tier shared by many users.
type Group struct {
	Base
	Name            string `gorm:"size:255" json:"name"`
	MaxStorage      int64  `json:"max_storage"` // bytes; 0 means unlimited
	SpeedLimit      int64  `json:"speed_limit"` // bytes/s for downloads; 0 means unlimited
	Permissions     JSON   `gorm:"type:json" json:"permissions"`
	StoragePolicyID uint   `json:"storage_policy_id"`
	// SSOGroups is a comma-separated list of SSO group/role names that map onto
	// this group: a user carrying any of them is assigned here at login.
	SSOGroups string `gorm:"size:1024" json:"sso_groups"`
	Settings  JSON   `gorm:"type:json" json:"-"`
}

// SSOGroupList returns the group's SSO group names, trimmed and non-empty.
func (g *Group) SSOGroupList() []string {
	var out []string
	for _, s := range strings.Split(g.SSOGroups, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// MatchesSSO reports whether any of the user's SSO groups map to this group.
func (g *Group) MatchesSSO(userGroups []string) bool {
	mine := g.SSOGroupList()
	for _, ug := range userGroups {
		for _, m := range mine {
			if strings.EqualFold(ug, m) {
				return true
			}
		}
	}
	return false
}

// GroupPermissions is the typed view of Group.Permissions. A nil flag means
// "unset": Share defaults to allowed, Admin defaults to denied.
type GroupPermissions struct {
	Share *bool `json:"share,omitempty"`
	Admin *bool `json:"admin,omitempty"`
}

// Perms decodes the group's permission flags.
func (g *Group) Perms() GroupPermissions {
	var p GroupPermissions
	_ = g.Permissions.Unmarshal(&p)
	return p
}

// CanShare reports whether members may create share links (default: allowed).
func (g *Group) CanShare() bool {
	p := g.Perms()
	return p.Share == nil || *p.Share
}

// CanAdmin reports whether members have administrator access (default: denied).
func (g *Group) CanAdmin() bool {
	p := g.Perms()
	return p.Admin != nil && *p.Admin
}
