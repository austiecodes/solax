package agenttest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/proc"
	"github.com/austiecodes/solax/pkg/ir"
)

// FakeCaps has only prompt and interrupt, so the daemon emulates
// follow_up with its queue.
var FakeCaps = ir.Caps{Native: map[ir.InputMode]bool{ir.ModePrompt: true, ir.ModeInterrupt: true}}

// FakeDriver is an in-process agent for tests. Each prompt runs one turn
// that answers with Reply(spec, text).
type FakeDriver struct {
	KindV ir.Kind
	// Reply returns the agent message for a prompt. It runs on the turn
	// goroutine, so it may call the server (as a Super Agent would).
	Reply func(spec agent.SessionSpec, text string) string
	// TurnDelay is the time a turn takes.
	TurnDelay time.Duration

	mu     sync.Mutex
	starts []agent.SessionSpec
}

func (d *FakeDriver) Kind() ir.Kind { return d.KindV }

func (d *FakeDriver) Probe(context.Context) (agent.Probe, error) {
	return agent.Probe{OK: true, Version: "fake", Caps: FakeCaps}, nil
}

// Starts returns the specs of all started sessions.
func (d *FakeDriver) Starts() []agent.SessionSpec {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]agent.SessionSpec(nil), d.starts...)
}

func (d *FakeDriver) Start(ctx context.Context, spec agent.SessionSpec) (agent.Session, error) {
	d.mu.Lock()
	d.starts = append(d.starts, spec)
	d.mu.Unlock()
	s := &fakeSession{d: d, spec: spec, em: proc.NewEmitter(spec.SessionID), ref: ir.SessionRef{Kind: d.KindV, NativeID: "native-" + spec.SessionID}}
	s.em.Emit(agent.SessionStarted(s.ref, spec.Model, FakeCaps))
	return s, nil
}

type fakeSession struct {
	d    *FakeDriver
	spec agent.SessionSpec
	em   *proc.Emitter
	ref  ir.SessionRef

	mu     sync.Mutex
	turns  int
	busy   bool
	closed bool
}

func (s *fakeSession) Ref() ir.SessionRef      { return s.ref }
func (s *fakeSession) Caps() ir.Caps           { return FakeCaps }
func (s *fakeSession) Events() <-chan ir.Event { return s.em.Events() }

func (s *fakeSession) Send(ctx context.Context, in ir.Input) (agent.Accepted, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.Mode == ir.ModeInterrupt {
		return agent.Accepted{}, nil
	}
	if in.Mode != ir.ModePrompt {
		return agent.Accepted{}, fmt.Errorf("fake: mode %q not native", in.Mode)
	}
	if s.busy {
		return agent.Accepted{}, fmt.Errorf("fake: prompt while busy")
	}
	s.busy = true
	s.turns++
	turn := fmt.Sprintf("turn-%d", s.turns)
	s.em.Emit(agent.UserMessage(in.Ref, in.Text))
	go func() {
		s.em.Emit(agent.TurnStarted(turn, ir.CauseInput))
		time.Sleep(s.d.TurnDelay)
		text := "ok"
		if s.d.Reply != nil {
			text = s.d.Reply(s.spec, in.Text)
		}
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
		s.em.Emit(
			agent.ItemCompleted(turn, ir.Item{ID: turn + ":msg", Kind: ir.ItemAgentMessage, Text: text}),
			agent.TurnCompleted(turn, ir.TurnOK, "", nil),
		)
	}()
	return agent.Accepted{}, nil
}

func (s *fakeSession) Resolve(context.Context, string, ir.Decision) error { return nil }

func (s *fakeSession) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.em.Emit(agent.SessionExited(s.ref, 0, "closed"))
		s.em.Close()
	}
	return nil
}
