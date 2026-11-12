// Package outbox runs the durable side effects that the Engine enqueues
// (the DSH Effects pattern). Each (kind, key) pair has one row. Effects run
// after commit, oldest first, with retry and exponential backoff.
//
// An effect is a signal: a handler reads current state from the store. A
// signal that arrives while its effect runs bumps the effect generation,
// so Complete requeues it instead of losing it (core.EffectRepo).
package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/austiecodes/solax/internal/core"
)

// Handler runs one claimed effect. Return Later to defer it without
// counting a failure, for example when the machine is offline. A handler
// that commits work in its own transaction checks tx.Effects().Holds(ef)
// there, so a run whose claim expired cannot commit.
type Handler func(ctx context.Context, ef core.Effect) error

// Later defers an effect without counting a failure.
type Later struct{ After time.Duration }

func (l Later) Error() string { return "later" }

// Options control one effect kind.
type Options struct {
	Concurrency int
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
}

// DefaultPoll is how often Run looks for due effects.
const DefaultPoll = 250 * time.Millisecond

const (
	defaultBackoff = time.Minute
	claimLimit     = 32
)

// Outbox processes due effects.
type Outbox struct {
	store    core.Store
	poll     time.Duration
	log      *slog.Logger
	now      core.Clock
	handlers map[core.EffectKind]registered
	kick     chan struct{}
}

type registered struct {
	run  Handler
	opts Options
}

// New returns an Outbox on store. It reads and writes effects only through
// core.Store, so it needs no concrete store type.
func New(store core.Store, log *slog.Logger) *Outbox {
	if log == nil {
		log = slog.Default()
	}
	return &Outbox{
		store: store, poll: DefaultPoll, log: log, now: time.Now,
		handlers: map[core.EffectKind]registered{},
		kick:     make(chan struct{}, 1),
	}
}

// Kick makes Run look for due effects now instead of at the next poll.
// The store calls it after a commit that enqueued effects. It never blocks.
func (o *Outbox) Kick() {
	select {
	case o.kick <- struct{}{}:
	default:
	}
}

// Handle registers the handler of one kind. Call it before Run.
func (o *Outbox) Handle(kind core.EffectKind, run Handler, opts Options) {
	o.handlers[kind] = registered{run: run, opts: opts.withDefaults()}
}

// Run processes due effects until ctx ends. Pending rows stay in the store.
func (o *Outbox) Run(ctx context.Context) error {
	t := time.NewTicker(o.poll)
	defer t.Stop()
	for {
		o.pump(ctx)
		select {
		case <-ctx.Done():
			// A stopped worker is not a failure of the server.
			return nil
		case <-t.C:
		case <-o.kick:
		}
	}
}

// pump runs all due effects of all kinds once. Kinds run in sorted order,
// so a test sees a stable order.
func (o *Outbox) pump(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()
	for _, kind := range slices.Sorted(maps.Keys(o.handlers)) {
		reg := o.handlers[kind]
		effects, err := o.claim(ctx, kind, reg.opts.Concurrency)
		if err != nil {
			if ctx.Err() == nil {
				o.log.Error("outbox: claim", "kind", kind, "err", err)
			}
			continue
		}
		for _, ef := range effects {
			wg.Add(1)
			go func() {
				defer wg.Done()
				o.run(ctx, kind, reg, ef)
			}()
		}
	}
}

func (o *Outbox) claim(ctx context.Context, kind core.EffectKind, n int) ([]core.Effect, error) {
	var out []core.Effect
	now := o.now()
	err := o.store.Atomic(ctx, func(tx core.Tx) error {
		var err error
		out, err = tx.Effects().ClaimDue(ctx, kind, now, min(n, claimLimit))
		return err
	})
	return out, err
}

// run runs one effect and records its outcome: done, deferred without a
// failure (Later), or failed with backoff.
func (o *Outbox) run(ctx context.Context, kind core.EffectKind, reg registered, ef core.Effect) {
	err := o.call(ctx, reg, ef)
	if err == nil {
		if err := o.complete(ctx, ef); err != nil && ctx.Err() == nil {
			o.log.Error("outbox: complete", "kind", kind, "key", ef.Key, "err", err)
		}
		return
	}
	attempts := ef.Attempts
	runAt := o.now().Add(backoff(reg.opts, attempts))
	var later Later
	switch {
	case errors.As(err, &later):
		runAt = o.now().Add(later.After)
	case ctx.Err() != nil:
		// The runner stops. Leave the effect claimed: ClaimTTL releases it.
		return
	default:
		attempts++
		o.log.Warn("outbox: effect failed", "kind", kind, "key", ef.Key, "attempts", attempts, "err", err)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := o.store.Atomic(ctx, func(tx core.Tx) error {
		return tx.Effects().Reschedule(ctx, ef, runAt, attempts, errText(err))
	}); err != nil && ctx.Err() == nil {
		o.log.Error("outbox: reschedule", "kind", kind, "key", ef.Key, "err", err)
	}
}

// call runs the handler and turns a panic into an error, so one bad effect
// cannot stop the outbox.
func (o *Outbox) call(ctx context.Context, reg registered, ef core.Effect) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return reg.run(ctx, ef)
}

func (o *Outbox) complete(ctx context.Context, ef core.Effect) error {
	removed := false
	err := o.store.Atomic(ctx, func(tx core.Tx) error {
		var err error
		removed, err = tx.Effects().Complete(ctx, ef)
		return err
	})
	if err == nil && !removed {
		// Signalled again during the run: it is due now.
		o.Kick()
	}
	return err
}

// backoff grows the delay with each failed attempt, up to MaxBackoff.
func backoff(opts Options, attempts int) time.Duration {
	d := opts.MinBackoff
	for range attempts {
		d = min(d*2, opts.MaxBackoff)
	}
	return d
}

func (o Options) withDefaults() Options {
	if o.Concurrency <= 0 {
		o.Concurrency = 4
	}
	if o.MinBackoff <= 0 {
		o.MinBackoff = time.Second
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = defaultBackoff
	}
	o.MinBackoff = min(o.MinBackoff, o.MaxBackoff)
	return o
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
