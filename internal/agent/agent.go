// Package agent defines the driver interface for coding agents. Code above
// this package does not know about drivers. It uses only the IR (pkg/ir).
package agent

import (
	"context"
	"fmt"

	"github.com/austiecodes/solax/pkg/ir"
)

// Driver starts sessions of one coding agent kind.
type Driver interface {
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

// Decoder translates native records into IR events. Each driver has one.
// It has no I/O, so golden tests can replay recorded transcripts through it.
type Decoder interface {
	Decode(record []byte) ([]ir.Event, error)
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
	Options      map[string]any // driver-specific, for example pi "codemode"
}

// ToolPolicy limits the tools of a session.
type ToolPolicy struct {
	Allow []string // native tool names; empty means the driver default
	Deny  []string
}

// Probe is the result of Driver.Probe.
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

// Registry holds the drivers of a daemon. Order is the registration order,
// so the Drivers list of a hello is stable from connect to connect.
type Registry struct {
	order []Driver
	byKey map[ir.Kind]Driver
}

// NewRegistry returns a registry with the given drivers.
func NewRegistry(ds ...Driver) *Registry {
	r := &Registry{order: make([]Driver, 0, len(ds)), byKey: make(map[ir.Kind]Driver, len(ds))}
	for _, d := range ds {
		r.order = append(r.order, d)
		r.byKey[d.Kind()] = d
	}
	return r
}

// Get returns the driver of a kind.
func (r *Registry) Get(k ir.Kind) (Driver, error) {
	d, ok := r.byKey[k]
	if !ok {
		return nil, fmt.Errorf("agent: no driver %q", k)
	}
	return d, nil
}

// All returns all drivers in registration order.
func (r *Registry) All() []Driver { return r.order }
