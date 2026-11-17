// Package agent defines the runtime interface for coding agents. Code above
// this package does not know about runtimes. It uses only the IR (pkg/ir).
package agent

import (
	"context"
	"fmt"

	"github.com/austiecodes/solax/pkg/ir"
)

// Runtime starts sessions of one coding agent kind.
type Runtime interface {
	Kind() ir.Kind
	// Probe checks the installed binary: version, auth, capabilities.
	Probe(ctx context.Context) (Probe, error)
	Start(ctx context.Context, spec SessionSpec) (Session, error)
}

// Session is one live agent process.
type Session interface {
	Ref() ir.SessionRef
	Caps() ir.Caps
	// Send delivers input. It returns after the agent accepted the input,
	// not after the agent finished the work.
	Send(ctx context.Context, in ir.Input) (Accepted, error)
	// Resolve answers an open request (approval or question).
	Resolve(ctx context.Context, requestID string, d ir.Decision) error
	// Events is closed when the process exits.
	Events() <-chan ir.Event
	// Close interrupts the running turn, then stops the process.
	Close(ctx context.Context) error
}

// SessionSpec describes a session to start.
type SessionSpec struct {
	SessionID    string         // Solax ses_ ID
	Resume       *ir.SessionRef // nil: new native session
	Dir          string
	Model        string
	Instructions string            // appended system prompt (Solax role + CLI etiquette)
	Env          map[string]string // SOLAX_* values
	Tools        ToolPolicy
	Extensions   []string       // pi only
	Options      map[string]any // runtime-specific, for example pi "codemode"
}

// ToolPolicy limits the tools of a session.
type ToolPolicy struct {
	Allow []string // native tool names; empty means the runtime default
	Deny  []string
}

// Probe is the result of Runtime.Probe.
type Probe struct {
	Version string
	OK      bool
	Problem string
	Caps    ir.Caps
}

// Accepted is the result of Session.Send.
type Accepted struct {
	// Emulated is true when the daemon queued the input because the agent
	// does not support the mode natively.
	Emulated bool
	// Duplicate is true when the input Ref was already accepted.
	Duplicate bool
}

// Find returns the runtime of kind k.
func Find(ds []Runtime, k ir.Kind) (Runtime, error) {
	for _, d := range ds {
		if d.Kind() == k {
			return d, nil
		}
	}
	return nil, fmt.Errorf("agent: no runtime %q", k)
}

// Option returns a string option of the spec, or def.
func (s SessionSpec) Option(key, def string) string {
	if v, ok := s.Options[key].(string); ok && v != "" {
		return v
	}
	return def
}
