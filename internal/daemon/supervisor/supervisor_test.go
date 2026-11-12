package supervisor

import (
	"context"
	"testing"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// fakeSession records Close calls and never emits events.
type fakeSession struct{ closed bool }

func (s *fakeSession) Ref() ir.SessionRef { return ir.SessionRef{} }
func (s *fakeSession) Caps() ir.Caps      { return ir.Caps{} }
func (s *fakeSession) Send(context.Context, ir.Input) (agent.Accepted, error) {
	return agent.Accepted{}, nil
}
func (s *fakeSession) Resolve(context.Context, string, ir.Decision) error { return nil }
func (s *fakeSession) Events() <-chan ir.Event                            { return nil }
func (s *fakeSession) Close(context.Context) error                        { s.closed = true; return nil }

type fakeSink struct{}

func (fakeSink) Send(context.Context, protocol.Envelope) error { return nil }

// UpdateSpecs keeps the sessions of surviving agents and closes the
// sessions of removed agents, without holding the lock while closing.
func TestUpdateSpecs(t *testing.T) {
	s := New([]AgentSpec{{Name: "a"}, {Name: "b"}}, 2, nil)

	keep, drop := &fakeSession{}, &fakeSession{}
	s.sessions["ses_1"] = &running{agentName: "a", sess: keep}
	s.sessions["ses_2"] = &running{agentName: "b", sess: drop}

	if err := s.UpdateSpecs(context.Background(), []AgentSpec{{Name: "a"}}, 1); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.sessions["ses_1"]; !ok {
		t.Fatal("the session of a surviving agent was removed")
	}
	if _, ok := s.sessions["ses_2"]; ok {
		t.Fatal("the session of a removed agent survived")
	}
	if !drop.closed {
		t.Fatal("the session of a removed agent was not closed")
	}
	if keep.closed {
		t.Fatal("the session of a surviving agent was closed")
	}
	if got := s.FreeSlots(); got != 0 {
		t.Fatalf("FreeSlots = %d, want 0 (1 session, 1 slot)", got)
	}
}

func TestFreeSlots(t *testing.T) {
	s := New(nil, 3, nil)
	if got := s.FreeSlots(); got != 3 {
		t.Fatalf("FreeSlots = %d, want 3", got)
	}
	s.sessions["x"] = &running{}
	if got := s.FreeSlots(); got != 2 {
		t.Fatalf("FreeSlots = %d, want 2", got)
	}
}
