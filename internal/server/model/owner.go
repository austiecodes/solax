package model

import (
	"context"
	"time"
)

// Owner is the person who owns an installation. v1 has one owner; the
// schema keeps owner_id so that team support can come later.
type Owner struct {
	ID        ID
	CreatedAt time.Time
}

// SaveOwner inserts an owner. An owner that exists is kept.
func SaveOwner(ctx context.Context, db Executor, o Owner) error {
	_, err := exec(ctx, db, `INSERT INTO owners(id, created_at) VALUES (?, ?)
		ON CONFLICT(id) DO NOTHING`, o.ID, ms(o.CreatedAt))
	return err
}

// GetDefaultOwner returns the owner of a single-owner installation, or
// errs.ErrNotFound before "solax server init".
func GetDefaultOwner(ctx context.Context, db Querier) (ID, error) {
	var id ID
	err := db.QueryRowContext(ctx, `SELECT id FROM owners ORDER BY created_at, id LIMIT 1`).Scan(&id)
	return id, mapErr(err)
}

// ListOwners returns all owners, oldest first.
func ListOwners(ctx context.Context, db Querier) ([]Owner, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, created_at FROM owners ORDER BY created_at, id`)
	return collect(rows, err, scanOwner)
}

func scanOwner(s scanner) (Owner, error) {
	var o Owner
	var created int64
	if err := s.Scan(&o.ID, &created); err != nil {
		return o, mapErr(err)
	}
	o.CreatedAt = tm(created)
	return o, nil
}
