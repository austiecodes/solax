package sqlite

import (
	"context"
	"encoding/json"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/pkg/ir"
)

// Sessions.

const sessionCols = `s.id, s.agent_id, s.job_id, s.state, s.ref, s.live, s.last_seq, s.updated_at`

func (t *txn) Session(ctx context.Context, id core.ID) (core.Session, error) {
	return scanSession(t.q.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions s WHERE s.id = ?`, id))
}

func (t *txn) SessionFor(ctx context.Context, agentID, jobID core.ID) (core.Session, error) {
	return scanSession(t.q.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions s WHERE s.agent_id = ? AND s.job_id = ?`, agentID, jobID))
}

func (t *txn) SaveSession(ctx context.Context, s core.Session) error {
	ref := ""
	if s.Ref != nil {
		b, err := json.Marshal(s.Ref)
		if err != nil {
			return err
		}
		ref = string(b)
	}
	_, err := t.exec(ctx, `INSERT INTO sessions(id, agent_id, job_id, state, ref, live, last_seq, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state = excluded.state, ref = excluded.ref, live = excluded.live,
			last_seq = excluded.last_seq, updated_at = excluded.updated_at`,
		s.ID, s.AgentID, s.JobID, s.State, ref, b2i(s.Live), s.LastSeq, ms(s.UpdatedAt))
	return err
}

func (t *txn) SessionsOnMachine(ctx context.Context, machineID core.ID) ([]core.Session, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+sessionCols+` FROM sessions s JOIN agents a ON a.id = s.agent_id
		WHERE a.machine_id = ? ORDER BY s.id`, machineID)
	return collect(rows, err, scanSession)
}

func scanSession(sc scanner) (core.Session, error) {
	var s core.Session
	var state, ref string
	var updated int64
	var live int
	if err := sc.Scan(&s.ID, &s.AgentID, &s.JobID, &state, &ref, &live, &s.LastSeq, &updated); err != nil {
		return s, mapErr(err)
	}
	s.State, s.Live, s.UpdatedAt = ir.SessionState(state), live != 0, tm(updated)
	if ref != "" {
		s.Ref = new(ir.SessionRef)
		if err := json.Unmarshal([]byte(ref), s.Ref); err != nil {
			return s, err
		}
	}
	return s, nil
}

// limitOr bounds a query limit: <= 0 means the default of 500, and no
// query returns more than 5000 rows.
func limitOr(n int) int {
	switch {
	case n <= 0:
		return 500
	case n > 5000:
		return 5000
	default:
		return n
	}
}

// Session events: the reliable IR events of each session.

func (t *txn) AddSessionEvent(ctx context.Context, sessionID core.ID, e ir.Event) (bool, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	res, err := t.exec(ctx, `INSERT INTO session_events(session_id, seq, type, payload) VALUES (?, ?, ?, ?)
		ON CONFLICT(session_id, seq) DO NOTHING`, sessionID, e.Seq, e.Type, string(b))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (t *txn) SessionEvents(ctx context.Context, sessionID core.ID, after int64, limit int) ([]ir.Event, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT payload FROM session_events WHERE session_id = ? AND seq > ? ORDER BY seq LIMIT ?`,
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
