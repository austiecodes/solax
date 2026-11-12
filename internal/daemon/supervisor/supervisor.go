// Package supervisor owns the agent sessions of one machine: start,
// resume, deliver, stop and the concurrency limit. It turns IR events into
// session.state and session.event records for the server.
package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/session"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// ErrUnknownSession is the ack error for a command to a session that does
// not run here. The server then starts the session again.
var ErrUnknownSession = errors.New("unknown session")

// Config is what the supervisor needs from the daemon.
type Config struct {
	Registry *agent.Registry
	// Prompt returns the role prompt of a session.
	Prompt func(role protocol.SessionRole) (string, error)
	// Env returns the environment of a session: SOLAX_* values and PATH.
	Env func(e protocol.SessionEnsure) map[string]string
	// Dir resolves the work folder of an agent, for example "~/src".
	Dir      func(string) string
	MaxSlots int
	Sink     Sink
	Log      *slog.Logger
}

// Sink receives records for the server.
type Sink interface {
	Send(ctx context.Context, env protocol.Envelope) error
}

// Supervisor runs sessions.
type Supervisor struct {
	cfg Config
	// base is the context of the session processes. A command context
	// ends with its ack; a session lives until it stops.
	base context.Context

	mu       sync.Mutex
	sessions map[string]*running
	starting map[string]chan struct{} // ensure under way, by session ID
}

// running is one live session with its state machine and input queue.
type running struct {
	id    string
	sess  agent.Session
	caps  ir.Caps
	super bool // the Super Agent session is never evicted

	// mu makes "check the state, then send or queue" and "apply an event,
	// then flush the queue" atomic. Without it, an input can pass the idle
	// check just as the turn ends, and wait until unrelated input arrives.
	mu       sync.Mutex
	state    *session.Machine
	queue    *session.Queue
	prompted bool      // a prompt was sent and its turn has not started yet
	active   time.Time // last input or event
}

// New returns a Supervisor. Sessions run under base.
func New(base context.Context, cfg Config) *Supervisor {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Dir == nil {
		cfg.Dir = func(s string) string { return s }
	}
	return &Supervisor{cfg: cfg, base: base, sessions: map[string]*running{}, starting: map[string]chan struct{}{}}
}

// SetMaxSlots changes the session limit. Running sessions are kept.
func (s *Supervisor) SetMaxSlots(n int) {
	s.mu.Lock()
	s.cfg.MaxSlots = n
	s.mu.Unlock()
}

// Handle dispatches one command from the server.
func (s *Supervisor) Handle(ctx context.Context, env protocol.Envelope) protocol.Ack {
	var err error
	switch env.Type {
	case protocol.TypeSessionEnsure:
		err = s.ensure(ctx, env.Payload)
	case protocol.TypeSessionDeliver:
		err = s.deliver(ctx, env.Payload)
	case protocol.TypeSessionStop:
		err = s.stop(ctx, env.Payload)
	case protocol.TypeSessionResolve:
		err = s.resolve(ctx, env.Payload)
	default:
		err = fmt.Errorf("%w: unknown command %q", errs.ErrInvalid, env.Type)
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
	return s.cfg.MaxSlots - len(s.sessions) - len(s.starting)
}

// States returns the state of each running session.
func (s *Supervisor) States() map[string]ir.SessionState {
	s.mu.Lock()
	all := maps.Clone(s.sessions)
	s.mu.Unlock()
	out := make(map[string]ir.SessionState, len(all))
	for id, r := range all {
		out[id] = r.state.State()
	}
	return out
}

// Shutdown interrupts and stops all sessions.
func (s *Supervisor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	all := make([]*running, 0, len(s.sessions))
	for _, r := range s.sessions {
		all = append(all, r)
	}
	s.mu.Unlock()
	var out []error
	for _, r := range all {
		if err := r.sess.Close(ctx); err != nil {
			out = append(out, fmt.Errorf("close %s: %w", r.id, err))
		}
	}
	return errors.Join(out...)
}

// ensure starts or resumes a session. A session that runs, or that is
// starting, is not started twice.
func (s *Supervisor) ensure(ctx context.Context, payload json.RawMessage) error {
	var e protocol.SessionEnsure
	if err := json.Unmarshal(payload, &e); err != nil || e.SessionID == "" {
		return fmt.Errorf("%w: bad session.ensure", errs.ErrInvalid)
	}
	s.mu.Lock()
	if _, ok := s.sessions[e.SessionID]; ok {
		s.mu.Unlock()
		return nil
	}
	if wait, ok := s.starting[e.SessionID]; ok {
		s.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
		if s.get(e.SessionID) == nil {
			return errors.New("session failed to start")
		}
		return nil
	}
	if len(s.sessions)+len(s.starting) >= s.cfg.MaxSlots {
		victim := s.idlest()
		s.mu.Unlock()
		if victim == nil {
			return fmt.Errorf("%w: no free session slot (max %d)", errs.ErrUnavailable, s.cfg.MaxSlots)
		}
		// Free the slot of the worker session that waited longest. A later
		// input resumes it from its native ref.
		s.cfg.Log.Info("evicting idle session", "session", victim.id)
		_ = victim.sess.Close(ctx)
		s.mu.Lock()
		delete(s.sessions, victim.id)
		if len(s.sessions)+len(s.starting) >= s.cfg.MaxSlots {
			s.mu.Unlock()
			return fmt.Errorf("%w: no free session slot (max %d)", errs.ErrUnavailable, s.cfg.MaxSlots)
		}
	}
	done := make(chan struct{})
	s.starting[e.SessionID] = done
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.starting, e.SessionID)
		s.mu.Unlock()
		close(done)
	}()

	drv, err := s.cfg.Registry.Get(e.Driver)
	if err != nil {
		return err
	}
	var prompt string
	if s.cfg.Prompt != nil {
		if prompt, err = s.cfg.Prompt(e.Role); err != nil {
			return err
		}
	}
	spec := agent.SessionSpec{
		SessionID: e.SessionID, Resume: e.Resume, Dir: s.cfg.Dir(e.Dir), Model: e.Model,
		Instructions: prompt, Options: e.Options,
	}
	if s.cfg.Env != nil {
		spec.Env = s.cfg.Env(e)
	}
	sess, err := drv.Start(ctx, spec)
	if err != nil && e.Resume != nil {
		// A native session that cannot resume (deleted, other version)
		// starts fresh; the inbox brings the new input.
		s.cfg.Log.Warn("resume failed; starting a new session", "session", e.SessionID, "err", err)
		spec.Resume = nil
		sess, err = drv.Start(ctx, spec)
	}
	if err != nil {
		return err
	}
	r := &running{id: e.SessionID, sess: sess, caps: sess.Caps(), super: e.Role == protocol.RoleSuper,
		state: session.NewMachine(), queue: session.NewQueue(), active: time.Now()}
	s.mu.Lock()
	s.sessions[e.SessionID] = r
	s.mu.Unlock()
	ref := sess.Ref()
	s.report(protocol.SessionStateReport{SessionID: e.SessionID, State: ir.SessionStarting, Ref: &ref})
	go s.pump(r)
	s.cfg.Log.Info("session started", "session", e.SessionID, "agent", e.Agent, "driver", e.Driver, "role", e.Role)
	return nil
}

// deliver sends input as soon as possible: a prompt when the session is
// idle, a native follow_up when it is busy, or the queue when the driver
// has no follow_up. Steer and interrupt pass through.
func (s *Supervisor) deliver(ctx context.Context, payload json.RawMessage) error {
	var d protocol.SessionDeliver
	if err := json.Unmarshal(payload, &d); err != nil {
		return fmt.Errorf("%w: bad session.deliver", errs.ErrInvalid)
	}
	r := s.get(d.SessionID)
	if r == nil {
		return fmt.Errorf("%s: %w", d.SessionID, ErrUnknownSession)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = time.Now()
	in := d.Input
	if in.Ref != "" && r.queue.Seen(in.Ref) {
		return nil
	}
	if in.Mode == ir.ModeFollowUp || in.Mode == ir.ModePrompt {
		switch {
		case r.idle():
			in.Mode = ir.ModePrompt
		case r.caps.Native[ir.ModeFollowUp] && r.state.State() != ir.SessionStarting:
			in.Mode = ir.ModeFollowUp
		default:
			// Starting, or no native follow_up: flush sends it as the
			// next prompt when the session turns idle.
			in.Mode = ir.ModeFollowUp
			return r.queue.Push(in)
		}
	}
	return r.send(ctx, in)
}

// idle reports whether a prompt can start a turn now. The caller holds mu.
func (r *running) idle() bool {
	return r.state.State() == ir.SessionIdle && !r.prompted
}

// send writes one input. The caller holds mu.
func (r *running) send(ctx context.Context, in ir.Input) error {
	if !r.caps.Native[in.Mode] {
		return fmt.Errorf("%w: driver has no mode %q", errs.ErrInvalid, in.Mode)
	}
	if _, err := r.sess.Send(ctx, in); err != nil {
		return err
	}
	r.queue.Remember(in.Ref)
	if in.Mode == ir.ModePrompt {
		r.prompted = true
	}
	return nil
}

// pump forwards the events of a session and flushes queued input when the
// session turns idle.
func (s *Supervisor) pump(r *running) {
	for e := range r.sess.Events() {
		r.mu.Lock()
		r.active = time.Now()
		if e.Type == ir.TurnStarted || e.Type == ir.TurnCompleted {
			r.prompted = false
		}
		st, err := r.state.Apply(e)
		if err != nil {
			s.cfg.Log.Debug("event out of order", "session", r.id, "type", e.Type, "err", err)
		}
		if st == ir.SessionIdle && !r.prompted {
			s.flush(r)
		}
		r.mu.Unlock()
		ch := protocol.ChannelReliable
		if !e.Type.Reliable() {
			ch = protocol.ChannelLive
		}
		if err := s.cfg.Sink.Send(s.base, envelope(protocol.TypeSessionEvent, protocol.SessionEvent{Channel: ch, Event: e})); err != nil {
			s.cfg.Log.Warn("event not queued", "session", r.id, "err", err)
		}
	}
	s.mu.Lock()
	if s.sessions[r.id] == r {
		delete(s.sessions, r.id)
	}
	s.mu.Unlock()
	s.report(protocol.SessionStateReport{SessionID: r.id, State: ir.SessionExited})
	s.cfg.Log.Info("session ended", "session", r.id)
}

// flush sends the queued inputs as one prompt. The caller holds r.mu.
func (s *Supervisor) flush(r *running) {
	queued, _ := r.queue.Drain(ir.SessionIdle)
	if len(queued) == 0 {
		return
	}
	in := ir.Input{Mode: ir.ModePrompt, Ref: queued[len(queued)-1].Ref}
	for i, q := range queued {
		if i > 0 {
			in.Text += "\n\n---\n\n"
		}
		in.Text += q.Text
		in.Images = append(in.Images, q.Images...)
	}
	if err := r.send(s.base, in); err != nil {
		s.cfg.Log.Warn("queued input not sent", "session", r.id, "err", err)
	}
}

func (s *Supervisor) stop(ctx context.Context, payload json.RawMessage) error {
	var st protocol.SessionStop
	if err := json.Unmarshal(payload, &st); err != nil {
		return fmt.Errorf("%w: bad session.stop", errs.ErrInvalid)
	}
	r := s.get(st.SessionID)
	if r == nil {
		return nil
	}
	return r.sess.Close(ctx)
}

func (s *Supervisor) resolve(ctx context.Context, payload json.RawMessage) error {
	var rs protocol.SessionResolve
	if err := json.Unmarshal(payload, &rs); err != nil {
		return fmt.Errorf("%w: bad session.resolve", errs.ErrInvalid)
	}
	r := s.get(rs.SessionID)
	if r == nil {
		return fmt.Errorf("%s: %w", rs.SessionID, ErrUnknownSession)
	}
	return r.sess.Resolve(ctx, rs.RequestID, rs.Decision)
}

// idlest returns the idle worker session with the oldest activity, or
// nil. The caller holds s.mu.
func (s *Supervisor) idlest() *running {
	var best *running
	var bestAt time.Time
	for _, r := range s.sessions {
		if r.super {
			continue
		}
		r.mu.Lock()
		idle, at := r.idle() && r.queue.Len() == 0, r.active
		r.mu.Unlock()
		if idle && (best == nil || at.Before(bestAt)) {
			best, bestAt = r, at
		}
	}
	return best
}

func (s *Supervisor) get(id string) *running {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *Supervisor) report(r protocol.SessionStateReport) {
	_ = s.cfg.Sink.Send(s.base, envelope(protocol.TypeSessionState, r))
}

func envelope(t protocol.MessageType, v any) protocol.Envelope {
	b, _ := json.Marshal(v)
	return protocol.Envelope{Type: t, Payload: b}
}
