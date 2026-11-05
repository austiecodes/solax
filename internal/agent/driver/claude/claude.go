// Package claude is the driver for Claude Code in stream-json mode
// (claude -p --input-format stream-json --output-format stream-json).
package claude

import (
	"context"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Caps is the capability set of Claude Code. Steer and follow_up depend on
// the priority field of stream-json user messages (verify in M0). If it is
// not supported, set them to false and the daemon emulates them.
var Caps = ir.Caps{
	Native: map[ir.InputMode]bool{
		ir.ModePrompt:    true,  // user message
		ir.ModeSteer:     true,  // user message, priority "next"
		ir.ModeFollowUp:  true,  // user message, priority "later"
		ir.ModeNote:      false, // emulated: prepend to the next input
		ir.ModeInterrupt: true,  // control_request subtype "interrupt"
	},
	Requests: true, // control_request subtype "can_use_tool"
	Deltas:   true, // --include-partial-messages
	Subagent: true, // parent_tool_use_id
}

// Config configures the driver.
type Config struct {
	Binary   string // default "claude"
	StateDir string // generated --settings files
}

// Driver starts Claude Code sessions.
type Driver struct {
	cfg Config
}

// New returns a Claude Code driver.
func New(cfg Config) *Driver {
	if cfg.Binary == "" {
		cfg.Binary = "claude"
	}
	return &Driver{cfg: cfg}
}

func (d *Driver) Kind() ir.Kind { return ir.KindClaude }

// Probe runs "claude --version". It reads system/init capabilities on the
// first session start.
func (d *Driver) Probe(ctx context.Context) (agent.Probe, error) {
	return agent.Probe{}, errs.ErrNotImplemented
}

// Start runs:
//
//	claude -p --input-format stream-json --output-format stream-json
//	   --verbose --include-partial-messages
//	   --permission-mode auto --permission-prompts none
//	   --allowedTools <list> --append-system-prompt-file <file>
//	   --settings <file> --plugin-dir <solax plugin> [--resume <id>]
//
// Stop with an interrupt (or SIGINT) before SIGTERM, or the turn stays open.
func (d *Driver) Start(ctx context.Context, spec agent.SessionSpec) (agent.Session, error) {
	return nil, errs.ErrNotImplemented
}

var _ agent.Driver = (*Driver)(nil)
