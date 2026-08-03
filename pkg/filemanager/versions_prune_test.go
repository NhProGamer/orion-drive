package filemanager

import "testing"

// TestSetMaxVersions covers the retention-cap config resolution: 0 → built-in
// default, negative → unlimited (internal 0), positive → the value.
func TestSetMaxVersions(t *testing.T) {
	m := &Manager{maxVersions: defaultMaxVersions}

	m.SetMaxVersions(0)
	if m.maxVersions != defaultMaxVersions {
		t.Errorf("0 → %d, want default %d", m.maxVersions, defaultMaxVersions)
	}
	m.SetMaxVersions(-1)
	if m.maxVersions != 0 {
		t.Errorf("-1 → %d, want 0 (unlimited)", m.maxVersions)
	}
	m.SetMaxVersions(3)
	if m.maxVersions != 3 {
		t.Errorf("3 → %d, want 3", m.maxVersions)
	}
}
