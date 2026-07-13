package model

// Group is a permission and quota tier shared by many users.
type Group struct {
	Base
	Name            string `gorm:"size:255" json:"name"`
	MaxStorage      int64  `json:"max_storage"` // bytes; 0 means unlimited
	SpeedLimit      int64  `json:"speed_limit"` // bytes/s for downloads; 0 means unlimited
	Permissions     JSON   `gorm:"type:json" json:"permissions"`
	StoragePolicyID uint   `json:"storage_policy_id"`
	Settings        JSON   `gorm:"type:json" json:"-"`
}

// GroupPermissions is the typed view of Group.Permissions. A nil flag means
// "unset", which the Can* helpers treat as allowed (permissive default).
type GroupPermissions struct {
	Share *bool `json:"share,omitempty"`
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
