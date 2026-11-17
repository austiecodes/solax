// Package model holds the domain types and the SQL functions that load and
// store them. Each SQL function takes the database to run on: ReadDB or
// WriteDB for one query, or the *sql.Tx of Atomic to combine several in
// one transaction.
//
// The database is SQLite in WAL mode, on a cgo-free driver
// (modernc.org/sqlite). One writer connection begins its transactions
// IMMEDIATE, so writes are serialised in the process and never fail on a
// lock upgrade. A pool of read-only connections sees consistent WAL
// snapshots without blocking the writer.
package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/austiecodes/solax/internal/errs"
)

// ReadDB and WriteDB are the database of the process. Open sets them
// once at start; tests open a new database for each test.
//
// Pass ReadDB to a one-shot read and WriteDB to a one-shot write. Inside
// Atomic or Snapshot, pass the *sql.Tx of the callback and never ReadDB or
// WriteDB: they do not see the writes of the open transaction.
var (
	ReadDB  *sql.DB // read-only connections
	WriteDB *sql.DB // exactly one connection; BEGIN IMMEDIATE
)

// Querier runs queries. *sql.DB and *sql.Tx implement it.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Executor runs statements. *sql.DB and *sql.Tx implement it.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// QueryExecutor runs queries and statements, for a function that reads
// and writes.
type QueryExecutor interface {
	Querier
	Executor
}

// Open opens (and creates) the database at path, runs the migrations and
// sets ReadDB and WriteDB. path must be a file: an in-memory database
// cannot be shared between the writer and the readers.
func Open(ctx context.Context, path string) error {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return fmt.Errorf("model: a file path is required: %w", errs.ErrInvalid)
	}
	w, err := sql.Open("sqlite", dsn(path, "immediate", false))
	if err != nil {
		return err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxLifetime(0)
	if err := migrate(ctx, w); err != nil {
		w.Close()
		return err
	}

	r, err := sql.Open("sqlite", dsn(path, "deferred", true))
	if err != nil {
		w.Close()
		return err
	}
	n := max(4, runtime.GOMAXPROCS(0))
	r.SetMaxOpenConns(n)
	r.SetMaxIdleConns(n)
	ReadDB, WriteDB = r, w
	return nil
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

// Close closes the database. ReadDB and WriteDB keep their values, so a
// late call gets a "database is closed" error, not a nil pointer panic.
func Close() error {
	var err error
	if ReadDB != nil {
		err = ReadDB.Close()
	}
	if WriteDB != nil {
		err = errors.Join(err, WriteDB.Close())
	}
	return err
}

// Atomic runs fn in one write transaction. Write transactions are
// serialised. fn must not call Atomic: it would wait for the one writer
// connection, which fn holds.
func Atomic(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := WriteDB.BeginTx(ctx, nil)
	if err != nil {
		return mapErr(err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return mapErr(tx.Commit())
}

// Snapshot runs fn in one read-only transaction: a consistent view over
// several queries that does not block the writer. Any write in fn fails.
// A single query needs no snapshot; pass ReadDB.
func Snapshot(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := ReadDB.BeginTx(ctx, nil)
	if err != nil {
		return mapErr(err)
	}
	defer tx.Rollback()
	return fn(tx)
}

// exec runs a statement and maps its error.
func exec(ctx context.Context, db Executor, query string, args ...any) (sql.Result, error) {
	res, err := db.ExecContext(ctx, query, args...)
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
	if se, ok := errors.AsType[*sqlite.Error](err); ok {
		switch se.Code() & 0xff {
		case sqlite3.SQLITE_CONSTRAINT:
			return fmt.Errorf("%w: %v", errs.ErrConflict, err)
		case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
			return fmt.Errorf("%w: %v", errs.ErrUnavailable, err)
		}
	}
	return err
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

// nonNil returns an empty slice for nil, so JSON is [] and not null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// scanner is *sql.Row or *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

// collect scans all rows with scan.
func collect[T any](rows *sql.Rows, err error, scan func(scanner) (T, error)) ([]T, error) {
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, mapErr(rows.Err())
}

// jsonText encodes v for a JSON TEXT column.
func jsonText(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
