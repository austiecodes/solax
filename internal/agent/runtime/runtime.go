// Package runtime lists all built-in coding agent runtimes.
// Adding a runtime means one line here plus its own package.
package runtime

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/runtime/claude"
	"github.com/austiecodes/solax/internal/agent/runtime/codex"
	"github.com/austiecodes/solax/internal/agent/runtime/pi"
	"github.com/austiecodes/solax/pkg/ir"
)

// All returns all built-in runtimes in a fixed order, so the Runtimes list of
// a hello is stable from connect to connect, then one Claude Code runtime
// for each fork in claudeLike: a binary name or path, whose base name is
// the kind. A path named claude is the binary of the built-in, as when the
// one on the PATH is a wrapper that does not stream. Each runtime keeps its
// per-session state under <stateDir>/<kind>. An empty stateDir keeps
// nothing on disk.
func All(stateDir string, claudeLike ...string) ([]agent.Runtime, error) {
	sub := func(k ir.Kind) string {
		if stateDir == "" {
			return ""
		}
		return filepath.Join(stateDir, string(k))
	}
	claudeBin := "" // the binary of the built-in, "" for the default
	var forks []agent.Runtime
	seen := map[ir.Kind]bool{}
	for _, bin := range claudeLike {
		k := ir.Kind(filepath.Base(bin))
		if !kindName.MatchString(string(k)) {
			return nil, fmt.Errorf("runtimes.claude_compatible: %q: the name of the binary must match %s", bin, kindName)
		}
		if seen[k] {
			return nil, fmt.Errorf("runtimes.claude_compatible: %q: %q is named twice", bin, k)
		}
		seen[k] = true
		if k == ir.KindClaude {
			if bin != string(k) {
				claudeBin = bin
			}
			continue
		}
		if slices.Contains(ir.BuiltinKinds, k) {
			return nil, fmt.Errorf("runtimes.claude_compatible: %q: %q is a built-in runtime that is not Claude Code", bin, k)
		}
		forks = append(forks, claude.New(claude.Config{Kind: k, Binary: bin, StateDir: sub(k), InlinePrompt: true}))
	}
	ds := []agent.Runtime{
		pi.New(pi.Config{StateDir: sub(ir.KindPi)}),
		codex.New(codex.Config{StateDir: sub(ir.KindCodex)}),
		claude.New(claude.Config{Binary: claudeBin, StateDir: sub(ir.KindClaude)}),
	}
	return append(ds, forks...), nil
}

// kindName is a runtime kind: the same rule as an agent name.
var kindName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
