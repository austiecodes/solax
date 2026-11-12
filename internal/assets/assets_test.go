package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/austiecodes/solax/pkg/protocol"
)

// The release must leave exactly the files a fresh machine needs: the role
// prompts, the solax skill and the pi-solax extension.
func TestRelease(t *testing.T) {
	dir := t.TempDir()
	if err := Release(dir); err != nil {
		t.Fatalf("Release: %v", err)
	}
	for _, p := range []string{
		filepath.Join("prompts", "leader.md"),
		filepath.Join("prompts", "member.md"),
		filepath.Join("prompts", "super.md"),
		filepath.Join("skills", "solax", "SKILL.md"),
		filepath.Join("extensions", "pi-solax", "src", "index.ts"),
	} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	// A second release (an upgrade) overwrites instead of failing.
	if err := Release(dir); err != nil {
		t.Fatalf("second Release: %v", err)
	}
}

func TestPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := Release(dir); err != nil {
		t.Fatal(err)
	}
	for role, want := range map[protocol.SessionRole]string{
		protocol.RoleLeader: "workspace leader",
		protocol.RoleMember: "workspace member",
		protocol.RoleSuper:  "Super Agent",
	} {
		got, err := Prompt(dir, role)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s prompt does not mention %q:\n%s", role, want, got)
		}
	}
	// An unknown role gets the member prompt, which is the safe default.
	got, err := Prompt(dir, "bogus")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "workspace member") {
		t.Errorf("unknown role did not fall back to member:\n%s", got)
	}
}

func TestExtension(t *testing.T) {
	dir := t.TempDir()
	if err := Release(dir); err != nil {
		t.Fatal(err)
	}
	if got, want := Extension(dir, "pi-solax"), filepath.Join(dir, "extensions", "pi-solax"); got != want {
		t.Fatalf("Extension = %q, want %q", got, want)
	}
	for _, p := range []string{"/abs/ext", "./relative", "unknown-name"} {
		if got := Extension(dir, p); got != p {
			t.Fatalf("Extension(%q) = %q, want it unchanged", p, got)
		}
	}
}
