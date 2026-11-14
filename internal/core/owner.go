package core

import (
	"context"
	"fmt"

	"github.com/austiecodes/solax/internal/errs"
)

// InitOwner creates the owner of a new installation. v1 has one owner, so a
// second call fails with ErrConflict. "solax server init" calls it on the
// server host; it needs no actor.
func (e *Engine) InitOwner(ctx context.Context) (Owner, error) {
	var o Owner
	err := e.store.Atomic(ctx, func(tx Tx) error {
		owners, err := tx.Owners().List(ctx)
		if err != nil {
			return err
		}
		if len(owners) > 0 {
			return fmt.Errorf("%w: already initialised", errs.ErrConflict)
		}
		now := e.clock()
		o = Owner{ID: NewIDAt(PrefixOwner, now), CreatedAt: now}
		return tx.Owners().Save(ctx, o)
	})
	return o, err
}
