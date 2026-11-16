package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/austiecodes/solax/pkg/protocol"
)

// The release must leave the role prompts that a fresh machine needs.
func TestRelease(t *testing.T) {
	dir := t.TempDir()
	if err := Release(dir); err != nil {
		t.Fatalf("Release: %v", err)
	}
	for _, p := range []string{
		filepath.Join("prompts", "worker.md"),
		filepath.Join("prompts", "super.md"),
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
		protocol.RoleWorker: "Role: Solax agent",
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
	// An unknown role gets the worker prompt, which is the safe default.
	got, err := Prompt(dir, "bogus")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Role: Solax agent") {
		t.Errorf("unknown role did not fall back to worker:\n%s", got)
	}
}
