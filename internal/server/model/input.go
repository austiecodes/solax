package model

import (
	"context"
	"strings"
	"time"
)

// Input is one entry in the inbox of a session. The delivery loop sends
// pending inputs in order. The ID is the ir.Input ref, so a redelivery
// after a lost ack is dropped by the daemon.
type Input struct {
	ID        ID
	SessionID ID
	Text      string
	Sent      bool
	CreatedAt time.Time
}

// AddInput stores an input of a session.
func AddInput(ctx context.Context, db Executor, in Input) error {
	_, err := exec(ctx, db, `INSERT INTO inputs(id, session_id, text, sent, created_at) VALUES (?, ?, ?, ?, ?)`,
		in.ID, in.SessionID, in.Text, b2i(in.Sent), ms(in.CreatedAt))
	return err
}

// ListPendingInputs returns the unsent inputs of a session, oldest first.
func ListPendingInputs(ctx context.Context, db Querier, sessionID ID) ([]Input, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, session_id, text, created_at FROM inputs
		WHERE session_id = ? AND sent = 0 ORDER BY created_at, id`, sessionID)
	return collect(rows, err, func(s scanner) (Input, error) {
		var in Input
		var created int64
		err := s.Scan(&in.ID, &in.SessionID, &in.Text, &created)
		in.CreatedAt = tm(created)
		return in, mapErr(err)
	})
}

// MarkInputsSent marks inputs as sent.
func MarkInputsSent(ctx context.Context, db Executor, ids []ID) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := exec(ctx, db, `UPDATE inputs SET sent = 1 WHERE id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	return err
}

// ListPendingSessions returns the sessions that have unsent inputs.
func ListPendingSessions(ctx context.Context, db Querier) ([]ID, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT session_id FROM inputs WHERE sent = 0 ORDER BY session_id`)
	return collect(rows, err, func(s scanner) (ID, error) {
		var id ID
		return id, mapErr(s.Scan(&id))
	})
}
