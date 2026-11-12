package session

import (
	"strings"
	"sync"

	"github.com/austiecodes/solax/pkg/ir"
)

// Queue holds inputs for modes that a driver does not support natively
// (for example follow_up on Codex, note on Claude Code). It also remembers
// accepted refs, so a redelivered batch is not sent twice. The delivery
// path and the event pump share it, so it takes a lock.
type Queue struct {
	mu      sync.Mutex
	pending []ir.Input
	seen    map[string]struct{}
}

// NewQueue returns an empty queue.
func NewQueue() *Queue { return &Queue{seen: map[string]struct{}{}} }

// Seen reports whether an input ref was accepted before.
func (q *Queue) Seen(ref string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.seen[ref]
	return ok
}

// Len returns the number of queued inputs.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// Remember records a ref that was sent directly, so a redelivery of the
// same batch is dropped.
func (q *Queue) Remember(ref string) {
	if ref == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seen[ref] = struct{}{}
}

// Push adds an emulated input. A repeated ref is ignored, so the caller can
// redeliver a batch after a reconnect.
func (q *Queue) Push(in ir.Input) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if in.Ref != "" {
		if _, ok := q.seen[in.Ref]; ok {
			return nil
		}
		q.seen[in.Ref] = struct{}{}
	}
	q.pending = append(q.pending, in)
	return nil
}

// Drain returns the inputs to send now that the session moved to state s.
// Notes are merged into one input in front of the next prompt. While the
// session is busy or waiting, nothing is due.
func (q *Queue) Drain(s ir.SessionState) ([]ir.Input, error) {
	if s != ir.SessionIdle && s != ir.SessionExited {
		return nil, nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil, nil
	}

	var (
		notes []string
		out   []ir.Input
	)
	for _, in := range q.pending {
		if in.Mode == ir.ModeNote {
			if t := strings.TrimSpace(in.Text); t != "" {
				notes = append(notes, t)
			}
			continue
		}
		out = append(out, in)
	}
	if len(notes) > 0 {
		out = append([]ir.Input{{Mode: ir.ModeNote, Text: strings.Join(notes, "\n\n")}}, out...)
	}
	q.pending = nil
	return out, nil
}
