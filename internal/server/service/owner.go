package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/austiecodes/solax/internal/server/model"

	"github.com/austiecodes/solax/internal/errs"
)

// InitOwner creates the owner of a new installation. v1 has one owner, so a
// second call fails with ErrConflict. "solax server init" calls it on the
// server host; it needs no actor.
func InitOwner(ctx context.Context) (model.Owner, error) {
	var o model.Owner
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		owners, err := model.ListOwners(ctx, tx)
		if err != nil {
			return err
		}
		if len(owners) > 0 {
			return fmt.Errorf("%w: already initialised", errs.ErrConflict)
		}
		now := clock()
		o = model.Owner{ID: model.NewIDAt(model.PrefixOwner, now), CreatedAt: now}
		return model.SaveOwner(ctx, tx, o)
	})
	return o, err
}
