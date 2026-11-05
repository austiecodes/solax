package session

import (
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// Queue holds inputs for modes that a driver does not support natively
// (for example follow_up on Codex, note on Claude Code). It also remembers
// accepted refs, so a redelivered batch is not sent twice.
type Queue struct {
	pending []ir.Input
	seen    map[string]struct{}
}

// NewQueue returns an empty queue.
func NewQueue() *Queue { return &Queue{seen: map[string]struct{}{}} }

// Seen reports whether an input ref was accepted before.
func (q *Queue) Seen(ref string) bool {
	_, ok := q.seen[ref]
	return ok
}

// Push adds an emulated input.
func (q *Queue) Push(in ir.Input) error {
	return errs.ErrNotImplemented
}

// Drain returns the inputs to send now that the session moved to state s.
// Notes are merged into the next prompt.
func (q *Queue) Drain(s ir.SessionState) ([]ir.Input, error) {
	return nil, errs.ErrNotImplemented
}
