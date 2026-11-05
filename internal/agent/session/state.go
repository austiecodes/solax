// Package session holds the driver-agnostic parts of a session: the state
// machine and the input queue for emulated modes.
package session

import (
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Machine derives the session state from IR events only.
//
//	starting → idle ⇄ busy → exited
//	busy → waiting → busy   (a request is open)
type Machine struct {
	state ir.SessionState
	turn  string // ID of the open turn
	open  map[string]struct{}
}

// NewMachine returns a machine in the starting state.
func NewMachine() *Machine {
	return &Machine{state: ir.SessionStarting, open: map[string]struct{}{}}
}

// State returns the current state.
func (m *Machine) State() ir.SessionState { return m.state }

// Apply moves the machine with one event. It returns an error when the
// event is not valid in the current state, for example a second
// turn.started before turn.completed.
func (m *Machine) Apply(e ir.Event) (ir.SessionState, error) {
	return m.state, errs.ErrNotImplemented
}
