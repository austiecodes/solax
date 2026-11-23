package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skills returns the skills of a Solax plugin as instructions. Codex has
// no plugins for one session, so the skill text goes into the developer
// instructions; it names the skill folder, so Codex can read the files
// that the skill refers to. It returns "" for no plugin, and for a plugin
// that cannot be read: the session still starts.
func skills(plugin string) string {
	if plugin == "" {
		return ""
	}
	files, _ := filepath.Glob(filepath.Join(plugin, "skills", "*", "SKILL.md"))
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		text, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\n\n# Skill %s\n\nThe files of this skill are in %s. Read them when the skill tells you to.\n\n%s",
			filepath.Base(filepath.Dir(f)), filepath.Dir(f), strings.TrimSpace(string(text)))
	}
	return b.String()
}
