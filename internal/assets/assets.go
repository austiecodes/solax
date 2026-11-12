// Package assets carries the files that every solax binary needs at run
// time (role prompts, the solax skill, the pi-solax extension), embedded so
// a single binary works on a machine that has no checkout.
package assets

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/austiecodes/solax/pkg/protocol"
)

// Only package.json and src of the extension: a node_modules left by a
// local npm install must not end up in the binary.
//
//go:embed prompts skills extensions/pi-solax/package.json extensions/pi-solax/src
var assets embed.FS

// Dir names inside the release directory.
const (
	PromptsDir   = "prompts"
	SkillsDir    = "skills"
	ExtensionDir = "extensions"
)

// Release writes the assets below dir. Existing files are overwritten, so
// an upgrade replaces a stale prompt. The daemon calls it on start and
// serves prompts/<role>.md from the result.
func Release(dir string) error {
	return fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// embed uses forward slashes; the release path uses the OS form.
		target := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := assets.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// Prompt returns the instructions for one session role. The role comes
// from the server per session (protocol.SessionEnsure); an unknown role
// gets the member prompt, which is the safe default.
func Prompt(dir string, role protocol.SessionRole) (string, error) {
	name := string(protocol.RoleMember)
	switch role {
	case protocol.RoleLeader, protocol.RoleSuper:
		name = string(role)
	}
	b, err := os.ReadFile(filepath.Join(dir, PromptsDir, name+".md"))
	if err != nil {
		return "", fmt.Errorf("assets: prompt %q: %w", name, err)
	}
	return string(b), nil
}

// SkillDir returns the released solax skill directory.
func SkillDir(dir string) string { return filepath.Join(dir, SkillsDir, "solax") }

// Extension resolves an extension entry of config.yaml. A bare name such
// as "pi-solax" means the embedded copy and resolves to the released path.
// An absolute or relative path is used as written.
func Extension(dir, name string) string {
	if name == "" || strings.ContainsRune(name, filepath.Separator) || strings.HasPrefix(name, ".") {
		return name
	}
	p := filepath.Join(dir, ExtensionDir, name)
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return name
	}
	return p
}
