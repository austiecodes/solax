package service

import "testing"

// A launched project is named after its task: ASCII words in lower case,
// with dashes, cut at a dash.
func TestSlug(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Add a README to the repo", "add-a-readme-to-the-repo"},
		{"  Fix ./auth: the failing test!  ", "fix-auth-the-failing-test"},
		{"给仓库加一个 README", "readme"},
		{"写一份说明", ""},
		{"Refactor the session supervisor and its tests", "refactor-the-session-supervisor-and-its"},
		{"abcdefghijklmnopqrstuvwxyz0123456789abcdefghij", "abcdefghijklmnopqrstuvwxyz0123456789abcd"},
	} {
		if got := slug(tc.in, 40); got != tc.want {
			t.Errorf("slug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
