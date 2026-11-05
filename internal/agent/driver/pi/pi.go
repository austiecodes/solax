// Package pi is the driver for pi in RPC mode (pi --mode rpc).
package pi

import (
	"context"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Caps is the capability set of pi. Note is native only when the pi-solax
// extension is loaded (verify in M0).
var Caps = ir.Caps{
	Native: map[ir.InputMode]bool{
		ir.ModePrompt:    true, // prompt
		ir.ModeSteer:     true, // steer
		ir.ModeFollowUp:  true, // follow_up
		ir.ModeNote:      true, // extension: sendMessage(..., {triggerTurn: false})
		ir.ModeInterrupt: true, // abort
	},
	Requests: false, // only extension_ui_request
	Deltas:   true,
	Subagent: false,
}

// Config configures the driver.
type Config struct {
	Binary   string // default "pi"
	StateDir string // session dirs and the isolated PI_CODING_AGENT_DIR
}

// Driver starts pi sessions.
type Driver struct {
	cfg Config
}

// New returns a pi driver.
func New(cfg Config) *Driver {
	if cfg.Binary == "" {
		cfg.Binary = "pi"
	}
	return &Driver{cfg: cfg}
}

func (d *Driver) Kind() ir.Kind { return ir.KindPi }

// Probe runs "pi --version" and checks the isolated agent dir.
func (d *Driver) Probe(ctx context.Context) (agent.Probe, error) {
	return agent.Probe{}, errs.ErrNotImplemented
}

// Start runs:
//
//	pi --mode rpc --session-dir <dir> --tools <list> --skill <solax skill>
//	   --append-system-prompt <file> [-e <ext>...]
//
// with PI_CODING_AGENT_DIR set to an isolated dir that links auth.json and
// models.json, and settings.json that holds codemode.mode.
func (d *Driver) Start(ctx context.Context, spec agent.SessionSpec) (agent.Session, error) {
	return nil, errs.ErrNotImplemented
}

var _ agent.Driver = (*Driver)(nil)
