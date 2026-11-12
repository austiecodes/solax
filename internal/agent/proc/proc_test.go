package proc

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
)

// Emit never blocks, even with no reader, and order and seq survive.
func TestEmitterOrderAndNoBlock(t *testing.T) {
	e := NewEmitter("ses_1")
	for range 10000 {
		e.Emit(ir.Event{Type: ir.ItemDelta})
	}
	e.Close()
	e.Emit(ir.Event{Type: ir.TurnStarted}) // after Close: dropped
	var n uint64
	for ev := range e.Events() {
		n++
		if ev.Seq != n || ev.SessionID != "ses_1" || ev.At.IsZero() {
			t.Fatalf("event %d = seq %d session %q", n, ev.Seq, ev.SessionID)
		}
	}
	if n != 10000 {
		t.Fatalf("got %d events, want 10000", n)
	}
}

// Stop ends a process that ignores stdin EOF and SIGTERM-free waits.
func TestStopKillsStuckProcess(t *testing.T) {
	p, err := Start(Config{Binary: "sh", Args: []string{"-c", "trap '' TERM; sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, p.Stdout); _, _ = p.Wait() }()
	start := time.Now()
	if err := p.Stop(context.Background(), 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Stop took %v", d)
	}
}
