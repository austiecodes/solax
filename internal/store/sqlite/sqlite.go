// Package sqlite implements core.Store on SQLite in WAL mode. It uses a
// cgo-free driver (modernc.org/sqlite). Postgres can come later behind the
// same port.
package sqlite

import (
	"context"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
)

// Store is the SQLite store.
type Store struct {
	path string
}

// Open opens the database at path and runs the migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	return nil, errs.ErrNotImplemented
}

// Atomic runs fn in one transaction.
func (s *Store) Atomic(ctx context.Context, fn func(tx core.Tx) error) error {
	return errs.ErrNotImplemented
}

// Close closes the database.
func (s *Store) Close() error { return nil }

// auth.Repo.

func (s *Store) ClientByHash(ctx context.Context, hash string) (auth.ClientToken, error) {
	return auth.ClientToken{}, errs.ErrNotImplemented
}

func (s *Store) SaveClient(ctx context.Context, t auth.ClientToken) error {
	return errs.ErrNotImplemented
}

func (s *Store) Clients(ctx context.Context, ownerID core.ID) ([]auth.ClientToken, error) {
	return nil, errs.ErrNotImplemented
}

func (s *Store) MachineByTokenHash(ctx context.Context, hash string) (core.Machine, error) {
	return core.Machine{}, errs.ErrNotImplemented
}

var (
	_ core.Store = (*Store)(nil)
	_ auth.Repo  = (*Store)(nil)
)
