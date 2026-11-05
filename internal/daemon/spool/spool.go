// Package spool keeps reliable records on disk while the server is not
// connected. The size is bounded (default 50 MiB per machine). Records are
// sent again in order after a reconnect.
package spool

import "github.com/austiecodes/solax/internal/errs"

// DefaultMaxBytes is the default size limit.
const DefaultMaxBytes = 50 << 20

// Spool is an append-only file queue.
type Spool struct {
	dir      string
	maxBytes int64
}

// Open opens the spool in dir.
func Open(dir string, maxBytes int64) (*Spool, error) {
	return nil, errs.ErrNotImplemented
}

// Append adds a record. It fails when the spool is full.
func (s *Spool) Append(record []byte) error { return errs.ErrNotImplemented }

// Peek returns the oldest record without removing it.
func (s *Spool) Peek() ([]byte, error) { return nil, errs.ErrNotImplemented }

// Pop removes the oldest record after the server acked it.
func (s *Spool) Pop() error { return errs.ErrNotImplemented }
