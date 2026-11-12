// Package pi is the driver for pi in RPC mode (pi --mode rpc).
package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/framing"
	"github.com/austiecodes/solax/internal/agent/proc"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Caps is the capability set of pi. Note needs the pi-solax extension; it
// is emulated until the extension ships.
var Caps = ir.Caps{
	Native: map[ir.InputMode]bool{
		ir.ModePrompt:    true,  // prompt
		ir.ModeSteer:     true,  // steer
		ir.ModeFollowUp:  true,  // follow_up
		ir.ModeNote:      false, // emulated: prepend to the next input
		ir.ModeInterrupt: true,  // abort
	},
	Requests: false, // extension dialogs are cancelled: nobody is there to answer
	Deltas:   true,
	Subagent: false,
}

// startTimeout bounds the get_state handshake after spawn.
const startTimeout = 30 * time.Second

// Config configures the driver.
type Config struct {
	Binary   string // default "pi"
	StateDir string // per-session folders: pi session files, prompt, logs
}

// Driver starts pi sessions.
type Driver struct {
	cfg Config
}

// New returns a pi driver.
func New(cfg Config) *Driver {
	if cfg.Binary == "" {
		cfg.Binary = "pi"
	}
	return &Driver{cfg: cfg}
}

func (d *Driver) Kind() ir.Kind { return ir.KindPi }

// Probe runs "pi --version".
func (d *Driver) Probe(ctx context.Context) (agent.Probe, error) {
	v, err := proc.Version(ctx, d.cfg.Binary)
	if err != nil {
		return agent.Probe{Problem: err.Error(), Caps: Caps}, nil
	}
	return agent.Probe{Version: v, OK: true, Caps: Caps}, nil
}

// Start runs:
//
//	pi --mode rpc [--session-dir <dir> | --session <file>] [--model M]
//	   [--tools <list>] [--append-system-prompt <file>] [-e <ext>...]
//
// then reads the session ID and file with get_state.
func (d *Driver) Start(ctx context.Context, spec agent.SessionSpec) (agent.Session, error) {
	dir, err := proc.NewSessionDir(d.cfg.StateDir, spec.SessionID)
	if err != nil {
		return nil, err
	}
	args := []string{"--mode", "rpc"}
	switch {
	case spec.Resume != nil && spec.Resume.Path != "":
		args = append(args, "--session", spec.Resume.Path)
	case dir.Path != "":
		args = append(args, "--session-dir", filepath.Join(dir.Path, "sessions"))
	default:
		args = append(args, "--no-session")
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if t := spec.Option("thinking", ""); t != "" {
		args = append(args, "--thinking", t)
	}
	if len(spec.Tools.Allow) > 0 {
		args = append(args, "--tools", strings.Join(spec.Tools.Allow, ","))
	}
	if len(spec.Tools.Deny) > 0 {
		args = append(args, "--exclude-tools", strings.Join(spec.Tools.Deny, ","))
	}
	if spec.Instructions != "" {
		// --append-system-prompt takes text or a file path.
		f, ok, err := dir.WriteFile("prompt.md", spec.Instructions)
		if err != nil {
			return nil, err
		}
		if !ok {
			f = spec.Instructions
		}
		args = append(args, "--append-system-prompt", f)
	}
	for _, e := range spec.Extensions {
		args = append(args, "-e", e)
	}

	logw := dir.Log()
	p, err := proc.Start(proc.Config{Binary: d.cfg.Binary, Args: args, Dir: spec.Dir, Env: spec.Env, Stderr: logw})
	if err != nil {
		return nil, err
	}
	s := &session{
		p: p, w: framing.NewWriter(p.Stdin), em: proc.NewEmitter(spec.SessionID), log: logw,
		dec: NewDecoder(), waiting: map[string]chan response{}, seen: map[string]bool{},
	}
	go s.pump()

	hctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	var st struct {
		SessionID   string `json:"sessionId"`
		SessionFile string `json:"sessionFile"`
		Model       *struct {
			ID string `json:"id"`
		} `json:"model"`
	}
	if err := s.command(hctx, map[string]any{"type": "get_state"}, &st); err != nil {
		_ = p.Stop(context.WithoutCancel(ctx), time.Second)
		return nil, fmt.Errorf("pi: get_state: %w (stderr: %s)", err, proc.LastLine(p.StderrTail()))
	}
	ref := ir.SessionRef{Kind: ir.KindPi, NativeID: st.SessionID, Path: st.SessionFile}
	s.mu.Lock()
	s.ref = ref
	s.mu.Unlock()
	model := spec.Model
	if st.Model != nil {
		model = st.Model.ID
	}
	s.em.Emit(agent.SessionStarted(ref, model, Caps))
	return s, nil
}

type response struct {
	ID      string          `json:"id"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

type session struct {
	ref ir.SessionRef
	p   *proc.Proc
	w   *framing.Writer
	em  *proc.Emitter
	log io.Writer

	send sync.Mutex // serializes Send

	mu      sync.Mutex // guards dec, waiting, seen, n
	dec     *Decoder
	waiting map[string]chan response
	seen    map[string]bool
	n       int
	exited  bool
}

func (s *session) Ref() ir.SessionRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ref
}
func (s *session) Caps() ir.Caps           { return Caps }
func (s *session) Events() <-chan ir.Event { return s.em.Events() }

// command writes a command with a fresh ID and waits for its response.
func (s *session) command(ctx context.Context, cmd map[string]any, out any) error {
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		return errs.ErrUnavailable
	}
	s.n++
	id := fmt.Sprintf("solax-%d", s.n)
	ch := make(chan response, 1)
	s.waiting[id] = ch
	s.mu.Unlock()

	cmd["id"] = id
	if err := s.w.Write(cmd); err != nil {
		s.forget(id)
		return err
	}
	select {
	case <-ctx.Done():
		s.forget(id)
		return ctx.Err()
	case r, ok := <-ch:
		if !ok {
			return errors.New("pi: process exited")
		}
		if !r.Success {
			return fmt.Errorf("pi: %s: %s", r.Command, r.Error)
		}
		if out != nil && len(r.Data) > 0 {
			return json.Unmarshal(r.Data, out)
		}
		return nil
	}
}

func (s *session) forget(id string) {
	s.mu.Lock()
	delete(s.waiting, id)
	s.mu.Unlock()
}

// Send maps an input mode to a pi command and waits for its response.
func (s *session) Send(ctx context.Context, in ir.Input) (agent.Accepted, error) {
	s.send.Lock()
	defer s.send.Unlock()
	s.mu.Lock()
	dup := in.Ref != "" && s.seen[in.Ref]
	s.mu.Unlock()
	if dup {
		return agent.Accepted{Duplicate: true}, nil
	}

	var cmd map[string]any
	switch in.Mode {
	case ir.ModeInterrupt:
		return agent.Accepted{}, s.command(ctx, map[string]any{"type": "abort"}, nil)
	case ir.ModePrompt:
		cmd = map[string]any{"type": "prompt", "message": in.Text}
	case ir.ModeSteer:
		cmd = map[string]any{"type": "steer", "message": in.Text}
	case ir.ModeFollowUp:
		cmd = map[string]any{"type": "follow_up", "message": in.Text}
	default:
		return agent.Accepted{}, fmt.Errorf("pi: mode %q: %w", in.Mode, errs.ErrInvalid)
	}
	if len(in.Images) > 0 {
		imgs := make([]map[string]any, len(in.Images))
		for i, img := range in.Images {
			imgs[i] = map[string]any{"type": "image", "data": img.Data, "mimeType": img.MimeType}
		}
		cmd["images"] = imgs
	}

	// pi may start the run before it answers the command, so the decoder
	// learns about a prompt first, and the user message goes out first.
	s.mu.Lock()
	s.n++
	id := in.Ref
	if id == "" {
		id = fmt.Sprintf("input-%d", s.n)
	}
	prompt := in.Mode == ir.ModePrompt
	if prompt {
		s.dec.NoteInput()
	}
	s.mu.Unlock()
	s.em.Emit(agent.UserMessage(id, in.Text))

	var res struct {
		Disposition string `json:"disposition"`
	}
	err := s.command(ctx, cmd, &res)
	s.mu.Lock()
	if prompt && (err != nil || res.Disposition == "handled") && s.dec.pending > 0 {
		s.dec.pending--
	}
	if err == nil && in.Ref != "" {
		s.seen[in.Ref] = true
	}
	s.mu.Unlock()
	if err != nil {
		s.em.Emit(agent.Notice("", "error", "input_rejected", err.Error()))
		return agent.Accepted{}, err
	}
	return agent.Accepted{}, nil
}

// Resolve is not supported: pi has no approval requests.
func (s *session) Resolve(ctx context.Context, requestID string, d ir.Decision) error {
	return fmt.Errorf("pi: no open requests: %w", errs.ErrInvalid)
}

// Close aborts a running turn, then closes stdin; pi shuts down in order.
func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	busy := s.dec.Busy()
	s.mu.Unlock()
	if busy {
		actx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_ = s.command(actx, map[string]any{"type": "abort"}, nil)
		cancel()
	}
	return s.p.Stop(ctx, 5*time.Second)
}

// dialogs are the extension UI methods that wait for an answer.
var dialogs = map[string]bool{"select": true, "confirm": true, "input": true, "editor": true}

func (s *session) pump() {
	r := framing.NewReader(s.p.Stdout)
	for {
		rec, err := r.Next()
		if err != nil {
			break
		}
		var head struct {
			Type   string `json:"type"`
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(rec, &head) != nil {
			continue
		}
		switch head.Type {
		case "response":
			var res response
			_ = json.Unmarshal(rec, &res)
			s.mu.Lock()
			ch, ok := s.waiting[res.ID]
			delete(s.waiting, res.ID)
			s.mu.Unlock()
			if ok {
				ch <- res
			}
			continue
		case "extension_ui_request":
			if dialogs[head.Method] {
				_ = s.w.Write(map[string]any{"type": "extension_ui_response", "id": head.ID, "cancelled": true})
				s.mu.Lock()
				turn := s.dec.TurnID()
				s.mu.Unlock()
				s.em.Emit(agent.Notice(turn, "warn", "dialog_cancelled", "extension dialog cancelled: "+head.Method))
			}
			continue
		}
		s.mu.Lock()
		evs, derr := s.dec.Decode(rec)
		s.mu.Unlock()
		if derr != nil {
			fmt.Fprintf(s.log, "solax: %v\n", derr)
			continue
		}
		s.em.Emit(evs...)
	}
	code, _ := s.p.Wait()
	s.mu.Lock()
	s.exited = true
	for id, ch := range s.waiting {
		close(ch)
		delete(s.waiting, id)
	}
	var tail []ir.Event
	if s.dec.Busy() {
		tail = append(tail, agent.TurnCompleted(s.dec.turn, ir.TurnFailed, "agent process exited", nil))
	}
	ref := s.ref
	s.mu.Unlock()
	s.em.Emit(append(tail, agent.SessionExited(ref, code, proc.LastLine(s.p.StderrTail())))...)
	s.em.Close()
}

var _ agent.Driver = (*Driver)(nil)
