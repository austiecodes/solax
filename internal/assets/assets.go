// Package assets carries the role prompts and the Solax plugins that every
// solax binary needs at run time, embedded so a single binary works on a
// machine that has no checkout.
package assets

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// all: keeps the dot directories of the plugin manifests
// (.claude-plugin, .codex-plugin).
//
//go:embed prompts all:plugins
var assets embed.FS

const (
	// PromptsDir is the prompts directory inside the release directory.
	PromptsDir = "prompts"
	// PluginsDir holds one Solax plugin for each runtime in the release
	// directory.
	PluginsDir = "plugins"
	// sharedPlugin holds the files of every plugin, such as the skills.
	sharedPlugin = "shared"
)

// pluginOf maps a runtime to the folder of its plugin. Forks of Claude
// Code get none: they may not take --plugin-dir.
var pluginOf = map[ir.Kind]string{
	ir.KindPi:     "pi",
	ir.KindClaude: "claudecode",
	ir.KindCodex:  "codex",
}

// Release writes the assets below dir. The daemon calls it on start, so an
// upgrade replaces stale files. The prompts are copied as they are. Each
// plugin is built from plugins/shared plus the files of its runtime; a
// runtime file wins over a shared file with the same path.
func Release(dir string) error {
	if err := copyTree(PromptsDir, filepath.Join(dir, PromptsDir)); err != nil {
		return err
	}
	out := filepath.Join(dir, PluginsDir)
	if err := os.RemoveAll(out); err != nil { // drop the files of an older release
		return err
	}
	for _, name := range pluginOf {
		target := filepath.Join(out, name)
		if err := copyTree(PluginsDir+"/"+sharedPlugin, target); err != nil {
			return err
		}
		if err := copyTree(PluginsDir+"/"+name, target); err != nil {
			return err
		}
	}
	return nil
}

// copyTree writes the embedded tree root to the directory target.
func copyTree(root, target string) error {
	return fs.WalkDir(assets, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.FromSlash(root), filepath.FromSlash(p))
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := assets.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}

// Plugin returns the folder of the released Solax plugin for a runtime, or
// "" when the runtime has none.
func Plugin(dir string, kind ir.Kind) string {
	name, ok := pluginOf[kind]
	if !ok {
		return ""
	}
	return filepath.Join(dir, PluginsDir, name)
}

// Prompt returns the instructions for one session role. The role comes
// from the server per session (protocol.SessionEnsure); an unknown role
// gets the worker prompt, which is the safe default.
func Prompt(dir string, role protocol.SessionRole) (string, error) {
	name := string(protocol.RoleWorker)
	if role == protocol.RoleSuper {
		name = string(role)
	}
	b, err := os.ReadFile(filepath.Join(dir, PromptsDir, name+".md"))
	if err != nil {
		return "", fmt.Errorf("assets: prompt %q: %w", name, err)
	}
	return string(b), nil
}
