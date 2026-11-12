// Package codex is the driver for Codex app-server (JSON-RPC 2.0 over
// stdio). See https://learn.chatgpt.com/docs/app-server.
package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/framing"
	"github.com/austiecodes/solax/internal/agent/proc"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Caps is the capability set of Codex app-server.
var Caps = ir.Caps{
	Native: map[ir.InputMode]bool{
		ir.ModePrompt:    true,  // turn/start
		ir.ModeSteer:     true,  // turn/steer
		ir.ModeFollowUp:  false, // emulated: turn/start after turn/completed
		ir.ModeNote:      false, // emulated; thread/inject_items takes raw Responses items
		ir.ModeInterrupt: true,  // turn/interrupt
	},
	Requests: false, // approvalPolicy "never"; stray requests are declined
	Deltas:   true,
	Subagent: false,
}

// startTimeout bounds initialize + thread/start.
const startTimeout = 60 * time.Second

// Config configures the driver.
type Config struct {
	Binary   string // default "codex"
	StateDir string // per-session folders: logs
}

// Driver starts Codex sessions. It runs one app-server process per
// session, for isolation and simple crash handling.
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

// Probe runs "codex --version".
func (d *Driver) Probe(ctx context.Context) (agent.Probe, error) {
	v, err := proc.Version(ctx, d.cfg.Binary)
	if err != nil {
		return agent.Probe{Problem: err.Error(), Caps: Caps}, nil
	}
	return agent.Probe{Version: v, OK: true, Caps: Caps}, nil
}

// Start runs "codex app-server", sends initialize + initialized, then
// thread/start (or thread/resume) with cwd, model, approvalPolicy "never"
// and sandbox "workspace-write".
func (d *Driver) Start(ctx context.Context, spec agent.SessionSpec) (agent.Session, error) {
	dir, err := proc.NewSessionDir(d.cfg.StateDir, spec.SessionID)
	if err != nil {
		return nil, err
	}
	logw := dir.Log()
	p, err := proc.Start(proc.Config{Binary: d.cfg.Binary, Args: []string{"app-server"}, Dir: spec.Dir, Env: spec.Env, Stderr: logw})
	if err != nil {
		return nil, err
	}
	s := &session{p: p, em: proc.NewEmitter(spec.SessionID), dec: NewDecoder(), log: logw, seen: map[string]bool{}}
	s.peer = framing.NewPeer(p.Stdout, p.Stdin, s)
	go s.run()

	hctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	thread, err := s.handshake(hctx, spec)
	if err != nil {
		_ = p.Stop(context.WithoutCancel(ctx), time.Second)
		return nil, fmt.Errorf("codex: %w (stderr: %s)", err, proc.LastLine(p.StderrTail()))
	}
	s.mu.Lock()
	s.thread = thread.ID
	s.ref = ir.SessionRef{Kind: ir.KindCodex, NativeID: thread.ID, Path: thread.Path}
	ref := s.ref
	s.mu.Unlock()
	model := thread.Model
	if model == "" {
		model = spec.Model
	}
	s.em.Emit(agent.SessionStarted(ref, model, Caps))
	return s, nil
}

type thread struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Model string `json:"model"`
}

func (s *session) handshake(ctx context.Context, spec agent.SessionSpec) (thread, error) {
	err := s.peer.Call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "solax", "title": "Solax", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": true},
	}, nil)
	if err != nil {
		return thread{}, fmt.Errorf("initialize: %w", err)
	}
	if err := s.peer.Notify(ctx, "initialized", map[string]any{}); err != nil {
		return thread{}, err
	}
	var res struct {
		Thread thread `json:"thread"`
	}
	if spec.Resume != nil && spec.Resume.NativeID != "" {
		err = s.peer.Call(ctx, "thread/resume", map[string]any{"threadId": spec.Resume.NativeID}, &res)
		if err == nil {
			return res.Thread, nil
		}
	}
	params := map[string]any{
		"cwd":            spec.Dir,
		"approvalPolicy": "never",
		"sandbox":        spec.Option("sandbox", "workspace-write"),
	}
	if spec.Model != "" {
		params["model"] = spec.Model
	}
	if spec.Instructions != "" {
		params["developerInstructions"] = spec.Instructions
	}
	if err := s.peer.Call(ctx, "thread/start", params, &res); err != nil {
		return thread{}, fmt.Errorf("thread/start: %w", err)
	}
	if res.Thread.ID == "" {
		return thread{}, fmt.Errorf("thread/start: no thread ID")
	}
	return res.Thread, nil
}

type session struct {
	p    *proc.Proc
	peer *framing.Peer
	em   *proc.Emitter
	log  io.Writer

	send sync.Mutex // serializes Send

	mu     sync.Mutex // guards the fields below
	ref    ir.SessionRef
	thread string
	turn   string // open turn
	dec    *Decoder
	seen   map[string]bool
	n      int
}

func (s *session) Ref() ir.SessionRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ref
}
func (s *session) Caps() ir.Caps           { return Caps }
func (s *session) Events() <-chan ir.Event { return s.em.Events() }

func (s *session) state() (threadID, turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thread, s.turn
}

// Send maps an input mode to turn/start, turn/steer or turn/interrupt.
func (s *session) Send(ctx context.Context, in ir.Input) (agent.Accepted, error) {
	s.send.Lock()
	defer s.send.Unlock()
	s.mu.Lock()
	dup := in.Ref != "" && s.seen[in.Ref]
	s.mu.Unlock()
	if dup {
		return agent.Accepted{Duplicate: true}, nil
	}
	th, turn := s.state()
	var err error
	switch in.Mode {
	case ir.ModeInterrupt:
		if turn == "" {
			return agent.Accepted{}, nil
		}
		return agent.Accepted{}, s.peer.Call(ctx, "turn/interrupt", map[string]any{"threadId": th, "turnId": turn}, nil)
	case ir.ModePrompt:
		err = s.peer.Call(ctx, "turn/start", map[string]any{"threadId": th, "input": input(in)}, nil)
	case ir.ModeSteer:
		if turn == "" {
			return agent.Accepted{}, fmt.Errorf("codex: steer with no open turn: %w", errs.ErrConflict)
		}
		err = s.peer.Call(ctx, "turn/steer", map[string]any{"threadId": th, "input": input(in), "expectedTurnId": turn}, nil)
	default:
		return agent.Accepted{}, fmt.Errorf("codex: mode %q: %w", in.Mode, errs.ErrInvalid)
	}
	if err != nil {
		return agent.Accepted{}, err
	}
	s.mu.Lock()
	s.n++
	id := in.Ref
	if id == "" {
		id = fmt.Sprintf("input-%d", s.n)
	} else {
		s.seen[in.Ref] = true
	}
	s.mu.Unlock()
	s.em.Emit(agent.UserMessage(id, in.Text))
	return agent.Accepted{}, nil
}

func input(in ir.Input) []map[string]any {
	out := []map[string]any{{"type": "text", "text": in.Text}}
	for _, img := range in.Images {
		out = append(out, map[string]any{"type": "image", "url": "data:" + img.MimeType + ";base64," + img.Data})
	}
	return out
}

// Resolve is not supported: the thread runs with approvalPolicy "never".
func (s *session) Resolve(ctx context.Context, requestID string, d ir.Decision) error {
	return fmt.Errorf("codex: no open requests: %w", errs.ErrInvalid)
}

// Close interrupts a running turn, then stops the process.
func (s *session) Close(ctx context.Context) error {
	if _, turn := s.state(); turn != "" {
		ictx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, _ = s.Send(ictx, ir.Input{Mode: ir.ModeInterrupt})
		cancel()
	}
	return s.p.Stop(ctx, 3*time.Second)
}

// HandleNotification decodes a notification and tracks the open turn.
func (s *session) HandleNotification(ctx context.Context, method string, params json.RawMessage, raw []byte) {
	s.mu.Lock()
	evs, err := s.dec.Notification(method, params)
	for _, e := range evs {
		switch e.Type {
		case ir.TurnStarted:
			s.turn = e.TurnID
		case ir.TurnCompleted:
			s.turn = ""
		}
	}
	s.mu.Unlock()
	if err != nil {
		fmt.Fprintf(s.log, "solax: %v\n", err)
		return
	}
	s.em.Emit(evs...)
}

// HandleRequest declines approvals: a worker runs without a person.
func (s *session) HandleRequest(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval",
		"execCommandApproval", "applyPatchApproval":
		_, turn := s.state()
		s.em.Emit(agent.Notice(turn, "warn", "approval_declined", "declined approval request "+method))
		return map[string]any{"decision": "decline"}, nil
	}
	return nil, &framing.RPCError{Code: -32601, Message: "solax: unsupported request " + method}
}

func (s *session) run() {
	if err := s.peer.Run(context.Background()); err != nil {
		fmt.Fprintf(s.log, "solax: rpc: %v\n", err)
	}
	code, _ := s.p.Wait()
	s.mu.Lock()
	ref, turn := s.ref, s.turn
	s.turn = ""
	s.mu.Unlock()
	var tail []ir.Event
	if turn != "" {
		tail = append(tail, agent.TurnCompleted(turn, ir.TurnFailed, "agent process exited", nil))
	}
	s.em.Emit(append(tail, agent.SessionExited(ref, code, proc.LastLine(s.p.StderrTail())))...)
	s.em.Close()
}

var _ agent.Driver = (*Driver)(nil)
