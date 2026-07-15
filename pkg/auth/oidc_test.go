package auth

import "testing"

func TestExtractStrings(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []string
	}{
		{"array of strings", []any{"a", "b"}, []string{"a", "b"}},
		{"single string", "solo", []string{"solo"}},
		{"typed slice", []string{"x"}, []string{"x"}},
		{"mixed skips non-strings", []any{"a", 1, "", "b"}, []string{"a", "b"}},
		{"nil", nil, nil},
		{"number", 42, nil},
	}
	for _, c := range cases {
		got := extractStrings(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}
