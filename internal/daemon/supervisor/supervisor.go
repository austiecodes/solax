// Package supervisor owns the agent sessions of one machine: start,
// resume, deliver, resolve, stop, idle eviction and the concurrency limit.
// It turns IR events into session.state and session.event records.
package supervisor

import (
	"context"
	"sync"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/session"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// AgentSpec is the local definition of one agent, as the supervisor needs it.
type AgentSpec struct {
	Name     string
	Template agent.SessionSpec // Dir, Model, Tools, Extensions, Options
	Driver   agent.Driver
}

// Sink receives records for the server.
type Sink interface {
	Send(ctx context.Context, env protocol.Envelope) error
}

// Supervisor runs sessions.
type Supervisor struct {
	agents   map[string]AgentSpec
	maxSlots int
	sink     Sink

	mu       sync.Mutex
	sessions map[string]*running
}

// running is one live session with its state machine and input queue.
type running struct {
	sess  agent.Session
	state *session.Machine
	queue *session.Queue
}

// New returns a Supervisor.
func New(agents []AgentSpec, maxSlots int, sink Sink) *Supervisor {
	m := make(map[string]AgentSpec, len(agents))
	for _, a := range agents {
		m[a.Name] = a
	}
	return &Supervisor{agents: m, maxSlots: maxSlots, sink: sink, sessions: map[string]*running{}}
}

// Handle dispatches one command from the server.
func (s *Supervisor) Handle(ctx context.Context, env protocol.Envelope) protocol.Ack {
	var err error
	switch env.Type {
	case protocol.TypeSessionEnsure:
		err = s.ensure(ctx, env)
	case protocol.TypeSessionDeliver:
		err = s.deliver(ctx, env)
	case protocol.TypeSessionResolve:
		err = s.resolve(ctx, env)
	case protocol.TypeSessionStop:
		err = s.stop(ctx, env)
	default:
		err = errs.ErrInvalid
	}
	if err != nil {
		return protocol.Ack{Error: err.Error()}
	}
	return protocol.Ack{OK: true}
}

// FreeSlots returns the number of sessions that can still start.
func (s *Supervisor) FreeSlots() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxSlots - len(s.sessions)
}

// Shutdown interrupts and stops all sessions.
func (s *Supervisor) Shutdown(ctx context.Context) error { return errs.ErrNotImplemented }

// ensure starts or resumes a session. It sets SOLAX_* env from the
// command and pumps its events to the sink.
func (s *Supervisor) ensure(ctx context.Context, env protocol.Envelope) error {
	return errs.ErrNotImplemented
}

// deliver sends a batch with the selected mode, or queues it when the
// driver emulates the mode.
func (s *Supervisor) deliver(ctx context.Context, env protocol.Envelope) error {
	return errs.ErrNotImplemented
}

func (s *Supervisor) resolve(ctx context.Context, env protocol.Envelope) error {
	return errs.ErrNotImplemented
}

func (s *Supervisor) stop(ctx context.Context, env protocol.Envelope) error {
	return errs.ErrNotImplemented
}
