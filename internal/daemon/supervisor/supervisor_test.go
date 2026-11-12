package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/internal/agent/agenttest"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// sink collects the records for the server.
type sink struct {
	mu  sync.Mutex
	evs []ir.Event
}

func (s *sink) Send(_ context.Context, env protocol.Envelope) error {
	if env.Type != protocol.TypeSessionEvent {
		return nil
	}
	var se protocol.SessionEvent
	_ = json.Unmarshal(env.Payload, &se)
	s.mu.Lock()
	s.evs = append(s.evs, se.Event)
	s.mu.Unlock()
	return nil
}

func (s *sink) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, e := range s.evs {
		if e.Type == ir.ItemCompleted && e.Item.Kind == ir.ItemAgentMessage {
			out = append(out, e.Item.Text)
		}
	}
	return out
}

func cmd(typ protocol.MessageType, v any) protocol.Envelope {
	b, _ := json.Marshal(v)
	return protocol.Envelope{Type: typ, Payload: b}
}

func wait(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
	}
}

// Input to an idle session is a prompt. Input to a busy session of a
// driver without native follow_up waits in the queue and goes out as one
// prompt when the turn ends. A redelivered ref is dropped.
func TestDeliverModes(t *testing.T) {
	fake := &agenttest.FakeDriver{KindV: ir.KindClaude, TurnDelay: 100 * time.Millisecond,
		Reply: func(_ agent.SessionSpec, text string) string { return "re: " + text }}
	sk := &sink{}
	ctx := context.Background()
	s := New(ctx, Config{Registry: agent.NewRegistry(fake), MaxSlots: 1, Sink: sk})

	ens := protocol.SessionEnsure{SessionID: "ses_1", Agent: "a", Driver: ir.KindClaude, Dir: "/x"}
	if ack := s.Handle(ctx, cmd(protocol.TypeSessionEnsure, ens)); !ack.OK {
		t.Fatal(ack.Error)
	}
	// Ensure is idempotent and does not take a second slot.
	if ack := s.Handle(ctx, cmd(protocol.TypeSessionEnsure, ens)); !ack.OK {
		t.Fatal(ack.Error)
	}
	wait(t, func() bool { return s.States()["ses_1"] == ir.SessionIdle })

	deliver := func(ref, text string) protocol.Ack {
		return s.Handle(ctx, cmd(protocol.TypeSessionDeliver, protocol.SessionDeliver{
			SessionID: "ses_1", Input: ir.Input{Mode: ir.ModeFollowUp, Text: text, Ref: ref}}))
	}
	if ack := deliver("in_1", "one"); !ack.OK {
		t.Fatal(ack.Error)
	}
	// Busy now: these two wait and go out together.
	for _, in := range [][2]string{{"in_2", "two"}, {"in_3", "three"}, {"in_2", "two"}} {
		if ack := deliver(in[0], in[1]); !ack.OK {
			t.Fatal(ack.Error)
		}
	}
	wait(t, func() bool { return len(sk.messages()) == 2 })
	got := sk.messages()
	if got[0] != "re: one" || !strings.Contains(got[1], "two") || !strings.Contains(got[1], "three") || strings.Count(got[1], "two") != 1 {
		t.Fatalf("agent messages = %q", got)
	}
	// A redelivery of a sent batch is dropped.
	if ack := deliver("in_1", "one"); !ack.OK {
		t.Fatal(ack.Error)
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(sk.messages()); n != 2 {
		t.Fatalf("redelivery ran another turn: %d messages", n)
	}

	// A deliver for a session that does not run asks the server to start it.
	ack := s.Handle(ctx, cmd(protocol.TypeSessionDeliver, protocol.SessionDeliver{SessionID: "ses_9"}))
	if ack.OK || !strings.Contains(ack.Error, ErrUnknownSession.Error()) {
		t.Fatalf("unknown session ack = %+v", ack)
	}
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// When all slots are full, a new session evicts the idle worker session
// that waited longest. The Super Agent session and busy sessions stay.
func TestEvictIdle(t *testing.T) {
	fake := &agenttest.FakeDriver{KindV: ir.KindClaude, TurnDelay: time.Second}
	ctx := context.Background()
	s := New(ctx, Config{Registry: agent.NewRegistry(fake), MaxSlots: 2, Sink: &sink{}})
	ensure := func(id string, role protocol.SessionRole) protocol.Ack {
		return s.Handle(ctx, cmd(protocol.TypeSessionEnsure, protocol.SessionEnsure{SessionID: id, Role: role, Driver: ir.KindClaude}))
	}
	if !ensure("super", protocol.RoleSuper).OK || !ensure("w1", protocol.RoleMember).OK {
		t.Fatal("start failed")
	}
	wait(t, func() bool { return s.States()["w1"] == ir.SessionIdle && s.States()["super"] == ir.SessionIdle })
	if ack := ensure("w2", protocol.RoleMember); !ack.OK {
		t.Fatalf("w2: %s", ack.Error)
	}
	st := s.States()
	if _, ok := st["w1"]; ok {
		t.Fatalf("idle worker w1 was not evicted: %v", st)
	}
	if _, ok := st["super"]; !ok {
		t.Fatal("the Super Agent session was evicted")
	}
	// w2 is busy and super is protected: no slot for w3.
	_ = s.Handle(ctx, cmd(protocol.TypeSessionDeliver, protocol.SessionDeliver{SessionID: "w2", Input: ir.Input{Mode: ir.ModeFollowUp, Text: "x"}}))
	if ack := ensure("w3", protocol.RoleMember); ack.OK {
		t.Fatal("w3 started although no session was idle")
	}
}
