package sqlite

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/pkg/ir"
)

// jsonText encodes v for a JSON TEXT column.
func jsonText(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// Jobs.

type jobRepo struct{ t *txn }

const jobCols = `id, owner_id, agent_id, session_id, requested_by, title, prompt, state, result, error, created_at, updated_at`

func (r jobRepo) Get(ctx context.Context, id core.ID) (core.Job, error) {
	return scanJob(r.t.q.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id = ?`, id))
}

func (r jobRepo) BySession(ctx context.Context, sessionID core.ID) (core.Job, error) {
	return scanJob(r.t.q.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE session_id = ?`, sessionID))
}

func (r jobRepo) Save(ctx context.Context, j core.Job) error {
	_, err := r.t.exec(ctx, `INSERT INTO jobs(`+jobCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state = excluded.state, result = excluded.result, error = excluded.error,
			title = excluded.title, updated_at = excluded.updated_at`,
		j.ID, j.OwnerID, j.AgentID, j.SessionID, j.RequestedBy, j.Title, j.Prompt, j.State, j.Result, j.Error,
		ms(j.CreatedAt), ms(j.UpdatedAt))
	return err
}

func (r jobRepo) List(ctx context.Context, ownerID core.ID, limit int) ([]core.Job, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE owner_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		ownerID, limitOr(limit))
	return collect(rows, err, scanJob)
}

func scanJob(s scanner) (core.Job, error) {
	var j core.Job
	var by, state string
	var created, updated int64
	if err := s.Scan(&j.ID, &j.OwnerID, &j.AgentID, &j.SessionID, &by, &j.Title, &j.Prompt, &state, &j.Result, &j.Error, &created, &updated); err != nil {
		return j, mapErr(err)
	}
	j.RequestedBy, j.State, j.CreatedAt, j.UpdatedAt = core.AuthorKind(by), core.JobState(state), tm(created), tm(updated)
	return j, nil
}

// Inputs.

type inputRepo struct{ t *txn }

func (r inputRepo) Add(ctx context.Context, in core.Input) error {
	_, err := r.t.exec(ctx, `INSERT INTO inputs(id, session_id, text, sent, created_at) VALUES (?, ?, ?, ?, ?)`,
		in.ID, in.SessionID, in.Text, b2i(in.Sent), ms(in.CreatedAt))
	return err
}

func (r inputRepo) Pending(ctx context.Context, sessionID core.ID) ([]core.Input, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT id, session_id, text, created_at FROM inputs
		WHERE session_id = ? AND sent = 0 ORDER BY created_at, id`, sessionID)
	return collect(rows, err, func(s scanner) (core.Input, error) {
		var in core.Input
		var created int64
		err := s.Scan(&in.ID, &in.SessionID, &in.Text, &created)
		in.CreatedAt = tm(created)
		return in, mapErr(err)
	})
}

func (r inputRepo) MarkSent(ctx context.Context, ids []core.ID) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := r.t.exec(ctx, `UPDATE inputs SET sent = 1 WHERE id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	return err
}

func (r inputRepo) PendingSessions(ctx context.Context) ([]core.ID, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT DISTINCT session_id FROM inputs WHERE sent = 0 ORDER BY session_id`)
	return collect(rows, err, func(s scanner) (core.ID, error) {
		var id core.ID
		return id, mapErr(s.Scan(&id))
	})
}

// Session events.

type sessionEventRepo struct{ t *txn }

func (r sessionEventRepo) Add(ctx context.Context, sessionID core.ID, e ir.Event) (bool, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	res, err := r.t.exec(ctx, `INSERT INTO session_events(session_id, seq, type, payload) VALUES (?, ?, ?, ?)
		ON CONFLICT(session_id, seq) DO NOTHING`, sessionID, e.Seq, e.Type, string(b))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (r sessionEventRepo) Since(ctx context.Context, sessionID core.ID, after int64, limit int) ([]ir.Event, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT payload FROM session_events WHERE session_id = ? AND seq > ? ORDER BY seq LIMIT ?`,
		sessionID, after, limitOr(limit))
	return collect(rows, err, func(s scanner) (ir.Event, error) {
		var e ir.Event
		var p string
		if err := s.Scan(&p); err != nil {
			return e, mapErr(err)
		}
		return e, json.Unmarshal([]byte(p), &e)
	})
}
