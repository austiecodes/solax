package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkills(t *testing.T) {
	plugin := t.TempDir()
	dir := filepath.Join(plugin, "skills", "solax-workflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: solax-workflow\n---\nRun workflows.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := skills(plugin)
	for _, want := range []string{"# Skill solax-workflow", "files of this skill are in " + dir, "Run workflows."} {
		if !strings.Contains(got, want) {
			t.Errorf("skills has no %q:\n%s", want, got)
		}
	}
	if skills("") != "" || skills(filepath.Join(plugin, "none")) != "" {
		t.Error("no plugin: want no text")
	}
}
