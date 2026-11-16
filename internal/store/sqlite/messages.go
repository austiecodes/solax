package sqlite

import (
	"context"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
)

// Messages: the Super Agent conversation of each owner.

type messageRepo struct{ t *txn }

const messageCols = `id, owner_id, seq, author_kind, author_id, kind, text, created_at`

func (r messageRepo) Get(ctx context.Context, id core.ID) (core.Message, error) {
	return scanMessage(r.t.q.QueryRowContext(ctx, `SELECT `+messageCols+` FROM messages WHERE id = ?`, id))
}

// Append assigns the next seq of the owner. Write transactions are
// serialised, so read-increment-write inside one is safe.
func (r messageRepo) Append(ctx context.Context, m core.Message) (core.Message, error) {
	if m.OwnerID == "" {
		return m, errs.ErrInvalid
	}
	err := r.t.q.QueryRowContext(ctx, `UPDATE owners SET super_seq = super_seq + 1 WHERE id = ? RETURNING super_seq`,
		m.OwnerID).Scan(&m.Seq)
	if err != nil {
		return m, mapErr(err)
	}
	_, err = r.t.exec(ctx, `INSERT INTO messages(`+messageCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.OwnerID, m.Seq, m.Author.Kind, m.Author.ID, m.Kind, m.Text, ms(m.CreatedAt))
	return m, err
}

func (r messageRepo) Since(ctx context.Context, ownerID core.ID, since int64, limit int) ([]core.Message, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+messageCols+` FROM messages WHERE owner_id = ? AND seq > ?
		ORDER BY seq LIMIT ?`, ownerID, since, limitOr(limit))
	return collect(rows, err, scanMessage)
}

func scanMessage(s scanner) (core.Message, error) {
	var m core.Message
	var authorKind, kind string
	var created int64
	if err := s.Scan(&m.ID, &m.OwnerID, &m.Seq, &authorKind, &m.Author.ID, &kind, &m.Text, &created); err != nil {
		return m, mapErr(err)
	}
	m.Author.Kind, m.Kind, m.CreatedAt = core.AuthorKind(authorKind), core.MessageKind(kind), tm(created)
	return m, nil
}
