package sqlite

import (
	"context"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/core"
)

// auth.Repo. Token lookups run on every request, outside any domain
// transaction, so they use the read pool; writes use the writer.

const clientCols = `id, owner_id, name, hash, created_at, last_used, revoked`

func (s *Store) ClientByHash(ctx context.Context, hash string) (auth.ClientToken, error) {
	return scanClient(s.r.QueryRowContext(ctx, `SELECT `+clientCols+` FROM client_tokens WHERE hash = ?`, hash))
}

func (s *Store) DefaultOwner(ctx context.Context) (core.ID, error) {
	var id core.ID
	err := s.r.QueryRowContext(ctx, `SELECT id FROM owners ORDER BY created_at, id LIMIT 1`).Scan(&id)
	return id, mapErr(err)
}

func (s *Store) ClientByID(ctx context.Context, id core.ID) (auth.ClientToken, error) {
	return scanClient(s.r.QueryRowContext(ctx, `SELECT `+clientCols+` FROM client_tokens WHERE id = ?`, id))
}

func (s *Store) SaveClient(ctx context.Context, t auth.ClientToken) error {
	_, err := s.w.ExecContext(ctx, `INSERT INTO client_tokens(`+clientCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, last_used = excluded.last_used, revoked = excluded.revoked`,
		t.ID, t.OwnerID, t.Name, t.Hash, ms(t.CreatedAt), ms(t.LastUsed), b2i(t.Revoked))
	return mapErr(err)
}

func (s *Store) Clients(ctx context.Context, ownerID core.ID) ([]auth.ClientToken, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+clientCols+` FROM client_tokens WHERE owner_id = ? ORDER BY created_at, id`, ownerID)
	return collect(rows, err, scanClient)
}

func (s *Store) MachineByTokenHash(ctx context.Context, hash string) (core.Machine, error) {
	return scanMachine(s.r.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE token_hash = ?`, hash))
}

func scanClient(sc scanner) (auth.ClientToken, error) {
	var t auth.ClientToken
	var created, used int64
	var revoked int
	if err := sc.Scan(&t.ID, &t.OwnerID, &t.Name, &t.Hash, &created, &used, &revoked); err != nil {
		return t, mapErr(err)
	}
	t.CreatedAt, t.LastUsed, t.Revoked = tm(created), tm(used), revoked != 0
	return t, nil
}

var _ auth.Repo = (*Store)(nil)
