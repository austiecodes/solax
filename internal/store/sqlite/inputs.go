package sqlite

import (
	"context"
	"strings"

	"github.com/austiecodes/solax/internal/core"
)

func (t *txn) AddInput(ctx context.Context, in core.Input) error {
	_, err := t.exec(ctx, `INSERT INTO inputs(id, session_id, text, sent, created_at) VALUES (?, ?, ?, ?, ?)`,
		in.ID, in.SessionID, in.Text, b2i(in.Sent), ms(in.CreatedAt))
	return err
}

func (t *txn) PendingInputs(ctx context.Context, sessionID core.ID) ([]core.Input, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT id, session_id, text, created_at FROM inputs
		WHERE session_id = ? AND sent = 0 ORDER BY created_at, id`, sessionID)
	return collect(rows, err, func(s scanner) (core.Input, error) {
		var in core.Input
		var created int64
		err := s.Scan(&in.ID, &in.SessionID, &in.Text, &created)
		in.CreatedAt = tm(created)
		return in, mapErr(err)
	})
}

func (t *txn) MarkInputsSent(ctx context.Context, ids []core.ID) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := t.exec(ctx, `UPDATE inputs SET sent = 1 WHERE id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	return err
}

func (t *txn) PendingSessions(ctx context.Context) ([]core.ID, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT DISTINCT session_id FROM inputs WHERE sent = 0 ORDER BY session_id`)
	return collect(rows, err, func(s scanner) (core.ID, error) {
		var id core.ID
		return id, mapErr(s.Scan(&id))
	})
}
