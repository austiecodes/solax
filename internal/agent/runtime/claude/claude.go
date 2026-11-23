// Package claude is the runtime for Claude Code in stream-json mode
// (claude -p --input-format stream-json --output-format stream-json), and
// for forks of it that speak the same protocol.
package claude

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/framing"
	"github.com/austiecodes/solax/internal/agent/proc"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Caps is the capability set of Claude Code. A user message written while
// a turn runs is queued by Claude Code and answered after the turn, so
// follow_up is native. Steer and note are emulated by the daemon.
var Caps = ir.Caps{
	Native: map[ir.InputMode]bool{
		ir.ModePrompt:    true,  // user message
		ir.ModeSteer:     false, // emulated as follow_up
		ir.ModeFollowUp:  true,  // user message, queued by Claude Code
		ir.ModeNote:      false, // emulated: prepend to the next input
		ir.ModeInterrupt: true,  // control_request subtype "interrupt"
	},
	Requests: false, // --permission-prompts none: nothing waits for a person
	Deltas:   true,  // --include-partial-messages
	Subagent: true,  // parent_tool_use_id
}

// Config configures the runtime.
type Config struct {
	// Kind names the runtime. A fork of Claude Code that speaks the same
	// stream-json is its own kind with its own binary.
	Kind     ir.Kind // default ir.KindClaude
	Binary   string  // default the kind
	StateDir string  // prompt files and session logs
	// InlinePrompt passes the instructions with --append-system-prompt,
	// never in a file. A fork that adds its own --append-system-prompt
	// fails on --append-system-prompt-file: Claude Code takes only one of
	// the two.
	InlinePrompt bool
}

// Runtime starts Claude Code sessions.
type Runtime struct {
	cfg Config

	// mu guards prompts: whether the binary of a fingerprint has
	// --permission-prompts. Forks and older versions do not.
	mu      sync.Mutex
	prompts map[string]bool
}

// New returns a Claude Code runtime.
func New(cfg Config) *Runtime {
	if cfg.Kind == "" {
		cfg.Kind = ir.KindClaude
	}
	if cfg.Binary == "" {
		cfg.Binary = string(cfg.Kind)
	}
	return &Runtime{cfg: cfg, prompts: map[string]bool{}}
}

func (d *Runtime) Kind() ir.Kind { return d.cfg.Kind }

// Fingerprint changes when the binary is installed, updated or removed.
func (d *Runtime) Fingerprint() string { return proc.Fingerprint(d.cfg.Binary) }

// Probe runs "claude --version", and "claude --help" to learn its flags.
func (d *Runtime) Probe(ctx context.Context) (agent.Probe, error) {
	v, err := proc.Version(ctx, d.cfg.Binary)
	if err != nil {
		return agent.Probe{Problem: err.Error(), Caps: Caps}, nil
	}
	d.hasPermissionPrompts(ctx)
	return agent.Probe{Version: v, OK: true, Caps: Caps}, nil
}

// hasPermissionPrompts tells whether the binary has --permission-prompts.
// Without it, print mode denies what would prompt, which is what "none"
// asks for. The answer is kept per fingerprint, so an update asks again.
func (d *Runtime) hasPermissionPrompts(ctx context.Context) bool {
	fp := d.Fingerprint()
	d.mu.Lock()
	ok, known := d.prompts[fp]
	d.mu.Unlock()
	if known {
		return ok
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := proc.Output(ctx, d.cfg.Binary, "--help")
	if err != nil {
		return true // the default of current Claude Code; do not keep a guess
	}
	ok = strings.Contains(string(out), "--permission-prompts")
	d.mu.Lock()
	d.prompts[fp] = ok
	d.mu.Unlock()
	return ok
}

// Start runs:
//
//	claude -p --input-format stream-json --output-format stream-json
//	   --verbose --include-partial-messages
//	   --permission-mode auto --permission-prompts none
//	   [--model M] [--allowedTools ...] [--append-system-prompt-file F]
//	   --session-id <uuid> | --resume <id>
//
// Solax picks the session ID, so the session ref is known before the first
// prompt. Claude Code prints system/init only after the first input.
func (d *Runtime) Start(ctx context.Context, spec agent.SessionSpec) (agent.Session, error) {
	native := newUUID()
	args := []string{
		"-p", "--input-format", "stream-json", "--output-format", "stream-json",
		"--verbose", "--include-partial-messages",
		"--permission-mode", spec.Option("permission_mode", "auto"),
	}
	if d.hasPermissionPrompts(ctx) {
		args = append(args, "--permission-prompts", "none")
	}
	if spec.Resume != nil && spec.Resume.NativeID != "" {
		native = spec.Resume.NativeID
		args = append(args, "--resume", native)
	} else {
		args = append(args, "--session-id", native)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if len(spec.Tools.Allow) > 0 {
		args = append(args, "--allowedTools", strings.Join(spec.Tools.Allow, ","))
	}
	if len(spec.Tools.Deny) > 0 {
		args = append(args, "--disallowedTools", strings.Join(spec.Tools.Deny, ","))
	}
	if spec.Plugin != "" {
		args = append(args, "--plugin-dir", spec.Plugin)
	}
	dir, err := proc.NewSessionDir(d.cfg.StateDir, spec.SessionID)
	if err != nil {
		return nil, err
	}
	if spec.Instructions != "" {
		f, ok, err := dir.WriteFile("prompt.md", spec.Instructions)
		switch {
		case err != nil:
			return nil, err
		case ok && !d.cfg.InlinePrompt:
			args = append(args, "--append-system-prompt-file", f)
		default:
			args = append(args, "--append-system-prompt", spec.Instructions)
		}
	}
	logw := dir.Log()

	p, err := proc.Start(proc.Config{Binary: d.cfg.Binary, Args: args, Dir: spec.Dir, Env: spec.Env, Stderr: logw})
	if err != nil {
		return nil, err
	}
	s := &session{
		ref: ir.SessionRef{Kind: d.cfg.Kind, NativeID: native},
		p:   p, w: framing.NewWriter(p.Stdin), em: proc.NewEmitter(spec.SessionID),
		dec: NewDecoder(), seen: map[string]bool{}, log: logw,
	}
	s.em.Emit(agent.SessionStarted(s.ref, spec.Model, Caps))
	go s.pump()
	return s, nil
}

type session struct {
	ref ir.SessionRef
	p   *proc.Proc
	w   *framing.Writer
	em  *proc.Emitter
	log io.Writer

	mu   sync.Mutex // guards dec, seen, inputs
	dec  *Decoder
	seen map[string]bool
	n    int
}

func (s *session) Ref() ir.SessionRef      { return s.ref }
func (s *session) Caps() ir.Caps           { return Caps }
func (s *session) Events() <-chan ir.Event { return s.em.Events() }

// Send writes a user message, or an interrupt control request.
func (s *session) Send(ctx context.Context, in ir.Input) (agent.Accepted, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.Ref != "" && s.seen[in.Ref] {
		return agent.Accepted{Duplicate: true}, nil
	}
	switch in.Mode {
	case ir.ModeInterrupt:
		if !s.dec.Busy() {
			return agent.Accepted{}, nil
		}
		s.n++
		s.dec.Interrupted()
		err := s.w.Write(map[string]any{
			"type": "control_request", "request_id": fmt.Sprintf("int-%d", s.n),
			"request": map[string]any{"subtype": "interrupt"},
		})
		return agent.Accepted{}, err
	case ir.ModePrompt, ir.ModeFollowUp:
	default:
		return agent.Accepted{}, fmt.Errorf("claude: mode %q: %w", in.Mode, errs.ErrInvalid)
	}
	s.n++
	id := in.Ref
	if id == "" {
		id = fmt.Sprintf("input-%d", s.n)
	}
	s.dec.NoteInput()
	if err := s.w.Write(userMessage(in)); err != nil {
		return agent.Accepted{}, err
	}
	if in.Ref != "" {
		s.seen[in.Ref] = true
	}
	s.em.Emit(agent.UserMessage(id, in.Text))
	return agent.Accepted{}, nil
}

func userMessage(in ir.Input) map[string]any {
	content := []map[string]any{}
	for _, img := range in.Images {
		content = append(content, map[string]any{
			"type":   "image",
			"source": map[string]any{"type": "base64", "media_type": img.MimeType, "data": img.Data},
		})
	}
	content = append(content, map[string]any{"type": "text", "text": in.Text})
	return map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}}
}

// Resolve is not supported: Claude Code runs with --permission-prompts none.
func (s *session) Resolve(ctx context.Context, requestID string, d ir.Decision) error {
	return fmt.Errorf("claude: no open requests: %w", errs.ErrInvalid)
}

// Close interrupts a running turn, then closes stdin and stops the process.
func (s *session) Close(ctx context.Context) error {
	_, _ = s.Send(ctx, ir.Input{Mode: ir.ModeInterrupt})
	return s.p.Stop(ctx, 3*time.Second)
}

// pump reads stdout until EOF, decodes each record and emits the events.
// It closes the event channel after session.exited.
func (s *session) pump() {
	r := framing.NewReader(s.p.Stdout)
	for {
		rec, err := r.Next()
		if err != nil {
			break
		}
		s.mu.Lock()
		evs, derr := s.dec.Decode(rec)
		turn := s.dec.TurnID()
		s.mu.Unlock()
		if derr != nil {
			// The record is lost. Show it in the transcript, not only in
			// stderr.log: a lost tool result leaves the tool spinning.
			fmt.Fprintf(s.log, "solax: %v\n", derr)
			s.em.Emit(agent.Notice(turn, "warn", "decode_error", derr.Error()))
			continue
		}
		s.em.Emit(evs...)
	}
	code, _ := s.p.Wait()
	s.mu.Lock()
	var tail []ir.Event
	if s.dec.Busy() {
		tail = append(tail, agent.TurnCompleted(s.dec.turn, ir.TurnFailed, "agent process exited", nil))
	}
	s.mu.Unlock()
	reason := s.p.StderrTail()
	s.em.Emit(append(tail, agent.SessionExited(s.ref, code, proc.LastLine(reason)))...)
	s.em.Close()
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
