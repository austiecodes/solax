package sqlite

import (
	"context"

	"github.com/austiecodes/solax/internal/core"
)

const jobCols = `id, owner_id, agent_id, session_id, requested_by, title, prompt, state, result, error, created_at, updated_at`

func (t *txn) Job(ctx context.Context, id core.ID) (core.Job, error) {
	return scanJob(t.q.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id = ?`, id))
}

func (t *txn) SaveJob(ctx context.Context, j core.Job) error {
	_, err := t.exec(ctx, `INSERT INTO jobs(`+jobCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state = excluded.state, result = excluded.result, error = excluded.error,
			title = excluded.title, updated_at = excluded.updated_at`,
		j.ID, j.OwnerID, j.AgentID, j.SessionID, j.RequestedBy, j.Title, j.Prompt, j.State, j.Result, j.Error,
		ms(j.CreatedAt), ms(j.UpdatedAt))
	return err
}

func (t *txn) Jobs(ctx context.Context, ownerID core.ID, limit int) ([]core.Job, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE owner_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
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
