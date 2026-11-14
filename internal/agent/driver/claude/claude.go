// Package claude is the driver for Claude Code in stream-json mode
// (claude -p --input-format stream-json --output-format stream-json).
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

// Config configures the driver.
type Config struct {
	Binary   string // default "claude"
	StateDir string // prompt files and session logs
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

// Probe runs "claude --version".
func (d *Driver) Probe(ctx context.Context) (agent.Probe, error) {
	v, err := proc.Version(ctx, d.cfg.Binary)
	if err != nil {
		return agent.Probe{Problem: err.Error(), Caps: Caps}, nil
	}
	return agent.Probe{Version: v, OK: true, Caps: Caps}, nil
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
func (d *Driver) Start(ctx context.Context, spec agent.SessionSpec) (agent.Session, error) {
	native := newUUID()
	args := []string{
		"-p", "--input-format", "stream-json", "--output-format", "stream-json",
		"--verbose", "--include-partial-messages",
		"--permission-mode", spec.Option("permission_mode", "auto"),
		"--permission-prompts", "none",
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
	dir, err := proc.NewSessionDir(d.cfg.StateDir, spec.SessionID)
	if err != nil {
		return nil, err
	}
	if spec.Instructions != "" {
		f, ok, err := dir.WriteFile("prompt.md", spec.Instructions)
		switch {
		case err != nil:
			return nil, err
		case ok:
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
		ref: ir.SessionRef{Kind: ir.KindClaude, NativeID: native},
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

var _ agent.Driver = (*Driver)(nil)
