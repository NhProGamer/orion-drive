package model

import "testing"

func TestSSOGroupList(t *testing.T) {
	g := Group{SSOGroups: " engineering ,  ops,, admins "}
	got := g.SSOGroupList()
	want := []string{"engineering", "ops", "admins"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestMatchesSSO(t *testing.T) {
	g := Group{SSOGroups: "engineering,ops"}
	cases := []struct {
		user []string
		want bool
	}{
		{[]string{"ops"}, true},
		{[]string{"OPS"}, true}, // case-insensitive
		{[]string{"marketing", "engineering"}, true},
		{[]string{"marketing"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := g.MatchesSSO(c.user); got != c.want {
			t.Errorf("MatchesSSO(%v) = %v, want %v", c.user, got, c.want)
		}
	}
	// A group with no mapping never matches.
	empty := Group{}
	if empty.MatchesSSO([]string{"anything"}) {
		t.Error("unmapped group should not match")
	}
}
