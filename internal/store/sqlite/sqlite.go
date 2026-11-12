// Package sqlite implements core.Store on SQLite in WAL mode. It uses a
// cgo-free driver (modernc.org/sqlite). Postgres can come later behind the
// same port.
//
// Concurrency model: one writer connection whose transactions begin
// IMMEDIATE, so write transactions are serialised in the process and never
// fail on a lock upgrade; and a pool of read-only connections that see
// consistent WAL snapshots without blocking the writer.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
)

// Store is the SQLite store.
type Store struct {
	path      string
	w         *sql.DB // exactly one connection; BEGIN IMMEDIATE
	r         *sql.DB // read-only connections
	onEffects func()
}

// Open opens (and creates) the database at path and runs the migrations.
// path must be a file: an in-memory database cannot be shared between the
// writer and the readers.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return nil, fmt.Errorf("sqlite: a file path is required: %w", errs.ErrInvalid)
	}
	w, err := sql.Open("sqlite", dsn(path, "immediate", false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxLifetime(0)

	s := &Store{path: path, w: w}
	if err := s.migrate(ctx); err != nil {
		w.Close()
		return nil, err
	}

	r, err := sql.Open("sqlite", dsn(path, "deferred", true))
	if err != nil {
		w.Close()
		return nil, err
	}
	n := max(4, runtime.GOMAXPROCS(0))
	r.SetMaxOpenConns(n)
	r.SetMaxIdleConns(n)
	s.r = r
	return s, nil
}

func dsn(path, txlock string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
	}
	q.Set("_txlock", txlock)
	return "file:" + path + "?" + q.Encode()
}

// OnEffects sets a function that runs after every commit that enqueued an
// effect. The server passes the Kick of its outbox, so effects run without
// waiting for the next poll. Call it before the store is used.
func (s *Store) OnEffects(fn func()) { s.onEffects = fn }

// Atomic runs fn in one write transaction.
func (s *Store) Atomic(ctx context.Context, fn func(tx core.Tx) error) (err error) {
	sqlTx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return mapErr(err)
	}
	t := &txn{q: sqlTx}
	defer func() {
		if p := recover(); p != nil {
			_ = sqlTx.Rollback()
			panic(p)
		}
	}()
	if err := fn(t); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return mapErr(err)
	}
	if t.enqueued && s.onEffects != nil {
		s.onEffects()
	}
	return nil
}

// Read runs fn in one read-only transaction.
func (s *Store) Read(ctx context.Context, fn func(tx core.Tx) error) error {
	sqlTx, err := s.r.BeginTx(ctx, nil)
	if err != nil {
		return mapErr(err)
	}
	defer sqlTx.Rollback()
	return fn(&txn{q: sqlTx, readOnly: true})
}

// Close closes the database.
func (s *Store) Close() error {
	var err error
	if s.r != nil {
		err = s.r.Close()
	}
	return errors.Join(err, s.w.Close())
}

// querier is the part of *sql.Tx that the repos use.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// txn implements core.Tx on one SQL transaction.
type txn struct {
	q        querier
	readOnly bool
	enqueued bool
}

func (t *txn) Owners() core.OwnerRepo               { return ownerRepo{t} }
func (t *txn) Machines() core.MachineRepo           { return machineRepo{t} }
func (t *txn) Agents() core.AgentRepo               { return agentRepo{t} }
func (t *txn) Workspaces() core.WorkspaceRepo       { return workspaceRepo{t} }
func (t *txn) Tasks() core.TaskRepo                 { return taskRepo{t} }
func (t *txn) Messages() core.MessageRepo           { return messageRepo{t} }
func (t *txn) TaskEvents() core.TaskEventRepo       { return eventRepo{t} }
func (t *txn) Sessions() core.SessionRepo           { return sessionRepo{t} }
func (t *txn) Cursors() core.CursorRepo             { return cursorRepo{t} }
func (t *txn) Effects() core.EffectRepo             { return effectRepo{t} }
func (t *txn) Jobs() core.JobRepo                   { return jobRepo{t} }
func (t *txn) Inputs() core.InputRepo               { return inputRepo{t} }
func (t *txn) SessionEvents() core.SessionEventRepo { return sessionEventRepo{t} }

func (t *txn) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	res, err := t.q.ExecContext(ctx, q, args...)
	return res, mapErr(err)
}

// mapErr maps driver errors to the error kinds of package errs.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return errs.ErrNotFound
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case sqlite3.SQLITE_CONSTRAINT:
			return fmt.Errorf("%w: %v", errs.ErrConflict, err)
		case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
			return fmt.Errorf("%w: %v", errs.ErrUnavailable, err)
		}
	}
	return err
}

// mustAffect turns "no row changed" into ErrNotFound.
func mustAffect(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// Times are stored as Unix milliseconds; 0 is the zero time.

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func tm(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

var (
	_ core.Store = (*Store)(nil)
	_ core.Tx    = (*txn)(nil)
)
