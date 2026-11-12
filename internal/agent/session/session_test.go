package session

import (
	"testing"

	"github.com/austiecodes/solax/pkg/ir"
)

func TestMachineTransitions(t *testing.T) {
	m := NewMachine()
	steps := []struct {
		typ ir.EventType
		req string
		in  ir.SessionState
		out ir.SessionState
		ok  bool
	}{
		{ir.SessionStarted, "", ir.SessionStarting, ir.SessionIdle, true},
		{ir.TurnStarted, "", ir.SessionIdle, ir.SessionBusy, true},
		{ir.TurnStarted, "", ir.SessionBusy, ir.SessionBusy, false}, // second turn
		{ir.RequestOpened, "r1", ir.SessionBusy, ir.SessionWaiting, true},
		{ir.TurnCompleted, "", ir.SessionWaiting, ir.SessionIdle, true},
		{ir.TurnStarted, "", ir.SessionIdle, ir.SessionBusy, true},
		{ir.TurnCompleted, "", ir.SessionBusy, ir.SessionIdle, true},
		{ir.SessionExitedEvent, "", ir.SessionIdle, ir.SessionExited, true},
		{ir.TurnStarted, "", ir.SessionExited, ir.SessionExited, false}, // dead session
	}
	for i, s := range steps {
		if got := m.State(); got != s.in {
			t.Fatalf("step %d: state = %q, want %q", i, got, s.in)
		}
		e := ir.Event{Type: s.typ, TurnID: "t1"}
		if s.req != "" {
			e.Request = &ir.Request{ID: s.req}
		}
		got, err := m.Apply(e)
		if s.ok && err != nil {
			t.Fatalf("step %d (%s): unexpected error %v", i, s.typ, err)
		}
		if !s.ok && err == nil {
			t.Fatalf("step %d (%s): want an error", i, s.typ)
		}
		if err == nil && got != s.out {
			t.Fatalf("step %d (%s): state = %q, want %q", i, s.typ, got, s.out)
		}
	}
}

func TestQueueDrain(t *testing.T) {
	q := NewQueue()
	if err := q.Push(ir.Input{Mode: ir.ModeFollowUp, Text: "a", Ref: "bat_1"}); err != nil {
		t.Fatal(err)
	}
	// A redelivered batch is ignored, not queued twice.
	if err := q.Push(ir.Input{Mode: ir.ModeFollowUp, Text: "a", Ref: "bat_1"}); err != nil {
		t.Fatal(err)
	}
	if err := q.Push(ir.Input{Mode: ir.ModeNote, Text: "n1", Ref: "bat_2"}); err != nil {
		t.Fatal(err)
	}
	if !q.Seen("bat_1") || q.Seen("bat_x") {
		t.Fatal("Seen is wrong")
	}

	if out, _ := q.Drain(ir.SessionBusy); out != nil {
		t.Fatalf("busy session drained %d inputs", len(out))
	}
	out, _ := q.Drain(ir.SessionIdle)
	if len(out) != 2 {
		t.Fatalf("got %d inputs, want 2", len(out))
	}
	if out[0].Mode != ir.ModeNote || out[1].Mode != ir.ModeFollowUp {
		t.Fatalf("order = %v, %v; want note first", out[0].Mode, out[1].Mode)
	}
	if out, _ := q.Drain(ir.SessionIdle); out != nil {
		t.Fatalf("second drain returned %d inputs", len(out))
	}
}

// Push and Drain from several goroutines must not race. Run with -race.
func TestQueueConcurrent(t *testing.T) {
	q := NewQueue()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 100 {
			_ = q.Push(ir.Input{Mode: ir.ModeFollowUp, Ref: string(rune('a' + i%26))})
			_, _ = q.Drain(ir.SessionIdle)
		}
	}()
	for range 100 {
		_ = q.Seen("bat_1")
	}
	<-done
}
