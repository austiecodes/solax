package sqlite

import (
	"context"

	"github.com/austiecodes/solax/internal/core"
)

func (t *txn) SaveOwner(ctx context.Context, o core.Owner) error {
	_, err := t.exec(ctx, `INSERT INTO owners(id, created_at) VALUES (?, ?)
		ON CONFLICT(id) DO NOTHING`, o.ID, ms(o.CreatedAt))
	return err
}

func (t *txn) Owners(ctx context.Context) ([]core.Owner, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT id, created_at FROM owners ORDER BY created_at, id`)
	return collect(rows, err, scanOwner)
}

func scanOwner(s scanner) (core.Owner, error) {
	var o core.Owner
	var created int64
	if err := s.Scan(&o.ID, &created); err != nil {
		return o, mapErr(err)
	}
	o.CreatedAt = tm(created)
	return o, nil
}
