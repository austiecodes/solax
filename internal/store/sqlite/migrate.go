package sqlite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// migrations returns the embedded migrations, sorted by version. A file is
// named NNNN_name.sql; versions start at 1 and have no gaps.
func migrations() ([]migration, error) {
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, f := range files {
		base := strings.TrimPrefix(f, "migrations/")
		num, _, ok := strings.Cut(base, "_")
		if !ok {
			return nil, fmt.Errorf("sqlite: migration %q: want NNNN_name.sql", base)
		}
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("sqlite: migration %q: %w", base, err)
		}
		b, err := migrationFS.ReadFile(f)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: base, sql: string(b)})
	}
	slices.SortFunc(out, func(a, b migration) int { return a.version - b.version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("sqlite: migration versions must be 1..n without gaps, got %d at %d", m.version, i+1)
		}
	}
	return out, nil
}

// migrate applies the migrations that are not applied yet, each in its own
// transaction. A database from a newer server is refused.
func (s *Store) migrate(ctx context.Context) error {
	ms, err := migrations()
	if err != nil {
		return err
	}
	if _, err := s.w.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("sqlite: migrations table: %w", err)
	}
	var cur int
	if err := s.w.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&cur); err != nil {
		return err
	}
	if cur > len(ms) {
		return fmt.Errorf("sqlite: database schema v%d is newer than this server (v%d)", cur, len(ms))
	}
	for _, m := range ms[cur:] {
		tx, err := s.w.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`,
			m.version, m.name, time.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
