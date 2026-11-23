package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/austiecodes/solax/pkg/ir"
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

// Each runtime gets a whole plugin: its manifest and the shared skills.
func TestReleasePlugins(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, PluginsDir, "pi", "skills", "old", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Release(dir); err != nil {
		t.Fatal(err)
	}
	for kind, manifest := range map[ir.Kind]string{
		ir.KindPi:     "package.json",
		ir.KindClaude: filepath.Join(".claude-plugin", "plugin.json"),
		ir.KindCodex:  filepath.Join(".codex-plugin", "plugin.json"),
	} {
		root := Plugin(dir, kind)
		for _, p := range []string{
			manifest,
			filepath.Join("skills", "solax-workflow", "SKILL.md"),
			filepath.Join("skills", "solax-workflow", "references", "workflow.d.ts"),
		} {
			if _, err := os.Stat(filepath.Join(root, p)); err != nil {
				t.Errorf("%s: %s: %v", kind, p, err)
			}
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a file of an older release is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, PluginsDir, sharedPlugin)); !os.IsNotExist(err) {
		t.Errorf("shared is a source folder, not a plugin: %v", err)
	}
	if got := Plugin(dir, "mycc"); got != "" {
		t.Errorf("a Claude fork got plugin %q, want none", got)
	}
}
