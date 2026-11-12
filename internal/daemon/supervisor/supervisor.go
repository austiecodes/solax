// Package supervisor owns the agent sessions of one machine: start,
// resume, deliver, resolve, stop, idle eviction and the concurrency limit.
// It turns IR events into session.state and session.event records.
package supervisor

import (
	"context"
	"fmt"
	"sync"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/session"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// AgentSpec is the local definition of one agent, as the supervisor needs it.
type AgentSpec struct {
	Name     string
	Template agent.SessionSpec // Dir, Model, Tools, Extensions, Options
	Driver   agent.Driver
	// Prompt returns the role prompt for a session of this agent. The
	// server decides the role per session; the daemon supplies the text.
	// nil: no extra prompt.
	Prompt func(role protocol.SessionRole) (string, error)
}

// Sink receives records for the server.
type Sink interface {
	Send(ctx context.Context, env protocol.Envelope) error
}

// Supervisor runs sessions.
type Supervisor struct {
	mu       sync.Mutex
	agents   map[string]AgentSpec
	maxSlots int
	sink     Sink
	sessions map[string]*running
}

// running is one live session with its state machine and input queue.
type running struct {
	agentName string

	// mu makes "transition the state, then flush the queue" and "check the
	// state, then queue an input" atomic. Without it, an input can pass the
	// idle check just as turn.completed makes the session idle, and stay in
	// the queue until unrelated input arrives.
	mu    sync.Mutex
	sess  agent.Session
	state *session.Machine
	queue *session.Queue
}

// New returns a Supervisor.
func New(agents []AgentSpec, maxSlots int, sink Sink) *Supervisor {
	return &Supervisor{agents: specMap(agents), maxSlots: maxSlots, sink: sink, sessions: map[string]*running{}}
}

// UpdateSpecs replaces the agent definitions and the slot limit, and stops
// the sessions of removed agents. It is safe to call while sessions run.
func (s *Supervisor) UpdateSpecs(ctx context.Context, agents []AgentSpec, maxSlots int) error {
	next := specMap(agents)

	s.mu.Lock()
	orphans := make([]*running, 0)
	for id, r := range s.sessions {
		if _, ok := next[r.agentName]; !ok {
			orphans = append(orphans, r)
			delete(s.sessions, id)
		}
	}
	s.agents, s.maxSlots = next, maxSlots
	s.mu.Unlock()

	// Close outside the lock: Close waits for the process, and an event
	// pump may need the lock.
	for _, r := range orphans {
		if err := r.sess.Close(ctx); err != nil {
			return fmt.Errorf("supervisor: close %s: %w", r.agentName, err)
		}
	}
	return nil
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
func (s *Supervisor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	all := make([]*running, 0, len(s.sessions))
	for _, r := range s.sessions {
		all = append(all, r)
	}
	s.sessions = map[string]*running{}
	s.mu.Unlock()

	var errsOut []error
	for _, r := range all {
		if err := r.sess.Close(ctx); err != nil {
			errsOut = append(errsOut, err)
		}
	}
	return joinErrs(errsOut)
}

// ensure starts or resumes a session. It sets SOLAX_* env from the
// command and pumps its events to the sink.
func (s *Supervisor) ensure(ctx context.Context, env protocol.Envelope) error {
	return errs.ErrNotImplemented
}

// deliver sends a batch with the selected mode, or queues it when the
// driver emulates the mode. State check and queue push happen under the
// session lock, so a turn boundary cannot strand the input.
func (s *Supervisor) deliver(ctx context.Context, env protocol.Envelope) error {
	return errs.ErrNotImplemented
}

func (s *Supervisor) resolve(ctx context.Context, env protocol.Envelope) error {
	return errs.ErrNotImplemented
}

func (s *Supervisor) stop(ctx context.Context, env protocol.Envelope) error {
	return errs.ErrNotImplemented
}

// apply moves the state machine and flushes the queue in one step. The
// event pump calls it, so a queued input goes out as soon as the session
// turns idle.
func (s *Supervisor) apply(ctx context.Context, r *running, e ir.Event) (ir.SessionState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.Apply(e)
}

func specMap(agents []AgentSpec) map[string]AgentSpec {
	m := make(map[string]AgentSpec, len(agents))
	for _, a := range agents {
		m[a.Name] = a
	}
	return m
}

func joinErrs(errsIn []error) error {
	if len(errsIn) == 0 {
		return nil
	}
	out := errsIn[0]
	for _, err := range errsIn[1:] {
		out = fmt.Errorf("%w; %w", out, err)
	}
	return out
}
