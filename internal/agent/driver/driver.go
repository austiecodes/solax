// Package driver lists all built-in coding agent drivers.
// Adding a driver means one line here plus its own package.
package driver

import (
	"path/filepath"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/driver/claude"
	"github.com/austiecodes/solax/internal/agent/driver/codex"
	"github.com/austiecodes/solax/internal/agent/driver/pi"
)

// All returns all built-in drivers in a fixed order, so the Drivers list of
// a hello is stable from connect to connect. Each driver keeps its
// per-session state under <stateDir>/<kind>. An empty stateDir keeps
// nothing on disk.
func All(stateDir string) []agent.Driver {
	sub := func(k string) string {
		if stateDir == "" {
			return ""
		}
		return filepath.Join(stateDir, k)
	}
	return []agent.Driver{
		pi.New(pi.Config{StateDir: sub("pi")}),
		codex.New(codex.Config{StateDir: sub("codex")}),
		claude.New(claude.Config{StateDir: sub("claude")}),
	}
}
