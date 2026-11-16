// Package assets carries the role prompts that every solax binary needs at
// run time, embedded so a single binary works on a machine that has no
// checkout.
package assets

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/austiecodes/solax/pkg/protocol"
)

//go:embed prompts
var assets embed.FS

// PromptsDir is the prompts directory inside the release directory.
const PromptsDir = "prompts"

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
