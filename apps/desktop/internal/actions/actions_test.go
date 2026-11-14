package actions

import "testing"

func TestIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	keys := map[string]string{}
	for _, a := range All {
		if seen[a.ID] {
			t.Fatalf("duplicate action %q", a.ID)
		}
		seen[a.ID] = true
		if a.Keys == "" {
			continue
		}
		if other, ok := keys[a.Keys]; ok {
			t.Fatalf("%q and %q share the keys %q", a.ID, other, a.Keys)
		}
		keys[a.Keys] = a.ID
	}
}

func TestGlyphs(t *testing.T) {
	for _, tc := range []struct {
		accel string
		mac   bool
		want  string
	}{
		{"CmdOrCtrl+Shift+L", true, "⇧⌘L"},
		{"CmdOrCtrl+Shift+L", false, "Ctrl+Shift+L"},
		{"CmdOrCtrl+,", true, "⌘,"},
		{"CmdOrCtrl++", true, "⌘+"},
		{"Alt+Enter", false, "Alt+Enter"},
		{"", true, ""},
	} {
		if got := glyphs(tc.accel, tc.mac); got != tc.want {
			t.Errorf("glyphs(%q, %v) = %q, want %q", tc.accel, tc.mac, got, tc.want)
		}
	}
}
