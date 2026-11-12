// Package session holds the driver-agnostic parts of a session: the state
// machine and the input queue for emulated modes.
package session

import (
	"sync"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Machine derives the session state from IR events only.
//
//	starting → idle ⇄ busy → exited
//	busy → waiting → busy   (a request is open)
//
// The event pump and the delivery path both read it, so every method takes
// the lock.
type Machine struct {
	mu    sync.Mutex
	state ir.SessionState
	turn  string // ID of the open turn
	open  map[string]struct{}
}

// NewMachine returns a machine in the starting state.
func NewMachine() *Machine {
	return &Machine{state: ir.SessionStarting, open: map[string]struct{}{}}
}

// State returns the current state.
func (m *Machine) State() ir.SessionState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Apply moves the machine with one event. It returns an error when the
// event is not valid in the current state, for example a second
// turn.started before turn.completed.
func (m *Machine) Apply(e ir.Event) (ir.SessionState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == ir.SessionExited {
		return m.state, errs.ErrConflict
	}
	switch e.Type {
	case ir.SessionStarted:
		if m.state != ir.SessionStarting {
			return m.state, errs.ErrConflict
		}
		m.state = ir.SessionIdle
	case ir.TurnStarted:
		if m.state != ir.SessionIdle {
			return m.state, errs.ErrConflict
		}
		m.state, m.turn = ir.SessionBusy, e.TurnID
	case ir.TurnCompleted:
		if m.state != ir.SessionBusy && m.state != ir.SessionWaiting {
			return m.state, errs.ErrConflict
		}
		// An unanswered request dies with the turn.
		m.state, m.turn, m.open = ir.SessionIdle, "", map[string]struct{}{}
	case ir.RequestOpened:
		if m.state != ir.SessionBusy || e.Request == nil || e.Request.ID == "" {
			return m.state, errs.ErrConflict
		}
		m.open[e.Request.ID] = struct{}{}
		m.state = ir.SessionWaiting
	case ir.RequestResolved:
		if m.state != ir.SessionWaiting || e.Request == nil {
			return m.state, errs.ErrConflict
		}
		delete(m.open, e.Request.ID)
		if len(m.open) == 0 {
			m.state = ir.SessionBusy
		}
	case ir.SessionExitedEvent:
		m.state, m.open = ir.SessionExited, map[string]struct{}{}
	}
	return m.state, nil
}
