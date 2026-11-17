package model

import (
	"context"
	"time"
)

// ClientToken is the stored record of a client token.
type ClientToken struct {
	ID        ID
	OwnerID   ID
	Name      string // for example "macbook-cli", "desktop"
	Hash      string
	CreatedAt time.Time
	LastUsed  time.Time
	Revoked   bool
}

const clientCols = `id, owner_id, name, hash, created_at, last_used, revoked`

// GetClientByHash returns the client token with this hash.
func GetClientByHash(ctx context.Context, db Querier, hash string) (ClientToken, error) {
	return scanClient(db.QueryRowContext(ctx, `SELECT `+clientCols+` FROM client_tokens WHERE hash = ?`, hash))
}

// GetClient returns the client token with this ID.
func GetClient(ctx context.Context, db Querier, id ID) (ClientToken, error) {
	return scanClient(db.QueryRowContext(ctx, `SELECT `+clientCols+` FROM client_tokens WHERE id = ?`, id))
}

// SaveClient inserts or updates a client token by ID.
func SaveClient(ctx context.Context, db Executor, t ClientToken) error {
	_, err := exec(ctx, db, `INSERT INTO client_tokens(`+clientCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, last_used = excluded.last_used, revoked = excluded.revoked`,
		t.ID, t.OwnerID, t.Name, t.Hash, ms(t.CreatedAt), ms(t.LastUsed), b2i(t.Revoked))
	return err
}

// ListClients returns the client tokens of an owner, oldest first.
func ListClients(ctx context.Context, db Querier, ownerID ID) ([]ClientToken, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+clientCols+` FROM client_tokens WHERE owner_id = ? ORDER BY created_at, id`, ownerID)
	return collect(rows, err, scanClient)
}

func scanClient(sc scanner) (ClientToken, error) {
	var t ClientToken
	var created, used int64
	var revoked int
	if err := sc.Scan(&t.ID, &t.OwnerID, &t.Name, &t.Hash, &created, &used, &revoked); err != nil {
		return t, mapErr(err)
	}
	t.CreatedAt, t.LastUsed, t.Revoked = tm(created), tm(used), revoked != 0
	return t, nil
}
