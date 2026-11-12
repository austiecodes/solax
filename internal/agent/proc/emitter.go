package proc

import (
	"sync"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
)

// Emitter stamps IR events (session ID, seq, time) and delivers them in
// order on a channel. Emit never blocks: events wait in an unbounded queue,
// so a slow consumer cannot stall the stdout reader of the agent, and a
// consumer may call Session.Send while it handles an event.
type Emitter struct {
	sessionID string
	now       func() time.Time

	mu     sync.Mutex
	seq    uint64
	queue  []ir.Event
	closed bool
	wake   chan struct{}
	out    chan ir.Event
}

// NewEmitter returns an Emitter and starts its forwarder.
func NewEmitter(sessionID string) *Emitter {
	e := &Emitter{sessionID: sessionID, now: time.Now, wake: make(chan struct{}, 1), out: make(chan ir.Event, 64)}
	go e.forward()
	return e
}

// Events returns the output channel. It is closed after Close, once all
// queued events went out.
func (e *Emitter) Events() <-chan ir.Event { return e.out }

// Emit stamps and queues events. Events after Close are dropped.
func (e *Emitter) Emit(evs ...ir.Event) {
	if len(evs) == 0 {
		return
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	for _, ev := range evs {
		e.seq++
		ev.Seq, ev.SessionID = e.seq, e.sessionID
		if ev.At.IsZero() {
			ev.At = e.now().UTC()
		}
		e.queue = append(e.queue, ev)
	}
	e.mu.Unlock()
	e.signal()
}

// Close flushes the queue and closes the channel. It is safe to call more
// than once.
func (e *Emitter) Close() {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	e.signal()
}

func (e *Emitter) signal() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Emitter) forward() {
	defer close(e.out)
	for {
		e.mu.Lock()
		batch, closed := e.queue, e.closed
		e.queue = nil
		e.mu.Unlock()
		for _, ev := range batch {
			e.out <- ev
		}
		if closed && len(batch) == 0 {
			return
		}
		if len(batch) == 0 {
			<-e.wake
		}
	}
}
