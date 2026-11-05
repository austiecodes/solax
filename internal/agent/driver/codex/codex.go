// Package codex is the driver for Codex app-server (JSON-RPC 2.0 over
// stdio).
package codex

import (
	"context"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Caps is the capability set of Codex app-server.
var Caps = ir.Caps{
	Native: map[ir.InputMode]bool{
		ir.ModePrompt:    true,  // turn/start
		ir.ModeSteer:     true,  // turn/steer
		ir.ModeFollowUp:  false, // emulated: turn/start after turn/completed
		ir.ModeNote:      true,  // thread/inject_items
		ir.ModeInterrupt: true,  // turn/interrupt
	},
	Requests: true, // item/*/requestApproval
	Deltas:   true,
	Subagent: true, // collabToolCall
}

// Config configures the driver.
type Config struct {
	Binary   string // default "codex"
	StateDir string // the isolated CODEX_HOME
}

// Driver starts Codex sessions. Phase 1 runs one app-server process per
// session.
type Driver struct {
	cfg Config
}

// New returns a Codex driver.
func New(cfg Config) *Driver {
	if cfg.Binary == "" {
		cfg.Binary = "codex"
	}
	return &Driver{cfg: cfg}
}

func (d *Driver) Kind() ir.Kind { return ir.KindCodex }

// Probe runs "codex --version" and checks the auth in CODEX_HOME.
func (d *Driver) Probe(ctx context.Context) (agent.Probe, error) {
	return agent.Probe{}, errs.ErrNotImplemented
}

// Start runs "codex app-server", sends initialize + initialized, then
// thread/start (or thread/resume) with cwd, model, approvalPolicy "never"
// and sandbox "workspaceWrite".
func (d *Driver) Start(ctx context.Context, spec agent.SessionSpec) (agent.Session, error) {
	return nil, errs.ErrNotImplemented
}

var _ agent.Driver = (*Driver)(nil)
