// Package driver builds the registry of all built-in coding agent drivers.
// Adding a driver means one line here plus its own package.
package driver

import (
	"path/filepath"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/driver/claude"
	"github.com/austiecodes/solax/internal/agent/driver/codex"
	"github.com/austiecodes/solax/internal/agent/driver/pi"
)

// Registry returns all built-in drivers. Each keeps its per-session state
// under <stateDir>/<kind>. An empty stateDir keeps nothing on disk.
func Registry(stateDir string) *agent.Registry {
	sub := func(k string) string {
		if stateDir == "" {
			return ""
		}
		return filepath.Join(stateDir, k)
	}
	return agent.NewRegistry(
		pi.New(pi.Config{StateDir: sub("pi")}),
		codex.New(codex.Config{StateDir: sub("codex")}),
		claude.New(claude.Config{StateDir: sub("claude")}),
	)
}
