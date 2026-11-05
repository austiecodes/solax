// Package outbox runs the durable side effects that the Engine enqueues
// (the DSH Effects pattern). Each (kind, key) pair has one row. Effects run
// after commit, oldest first, with retry and exponential backoff.
package outbox

import (
	"context"
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
)

// Handler runs one effect. Return Later to defer it without counting a
// failure, for example when the machine is offline.
type Handler func(ctx context.Context, key string, payload json.RawMessage) error

// Later defers an effect without counting a failure.
type Later struct{ After time.Duration }

func (l Later) Error() string { return "later" }

// Options control one effect kind.
type Options struct {
	Concurrency int
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
}

// Outbox processes due effects.
type Outbox struct {
	store    core.Store
	handlers map[core.EffectKind]registered
}

type registered struct {
	run  Handler
	opts Options
}

// New returns an Outbox on store.
func New(store core.Store) *Outbox {
	return &Outbox{store: store, handlers: map[core.EffectKind]registered{}}
}

// Handle registers the handler of one kind. Call it before Run.
func (o *Outbox) Handle(kind core.EffectKind, run Handler, opts Options) {
	o.handlers[kind] = registered{run: run, opts: opts}
}

// Run processes due effects until ctx ends. Pending rows stay in the store.
func (o *Outbox) Run(ctx context.Context) error {
	return errs.ErrNotImplemented
}
