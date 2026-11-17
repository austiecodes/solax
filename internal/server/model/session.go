package model

import (
	"context"
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
)

// Session is one agent process: the session of a job, or the Super Agent
// session. The daemon owns the
// process. The server keeps its state and its native ref.
type Session struct {
	ID        ID
	AgentID   ID
	JobID     ID // the job of a worker session; empty for the Super Agent session
	State     ir.SessionState
	Ref       *ir.SessionRef
	Live      bool  // the daemon reports the process as running
	LastSeq   int64 // last stored IR event
	UpdatedAt time.Time
}

// SessionStopped is the state of a session that has no process and never
// had one, or whose process ended. An input wakes it again.
const SessionStopped ir.SessionState = "stopped"

// Sessions.

const sessionCols = `s.id, s.agent_id, s.job_id, s.state, s.ref, s.live, s.last_seq, s.updated_at`

// GetSession returns the session with this ID.
func GetSession(ctx context.Context, db Querier, id ID) (Session, error) {
	return scanSession(db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions s WHERE s.id = ?`, id))
}

// GetSessionFor returns the session of an agent for a job ("" for the
// Super Agent).
func GetSessionFor(ctx context.Context, db Querier, agentID, jobID ID) (Session, error) {
	return scanSession(db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions s WHERE s.agent_id = ? AND s.job_id = ?`, agentID, jobID))
}

// SaveSession inserts or updates a session by ID.
func SaveSession(ctx context.Context, db Executor, s Session) error {
	ref := ""
	if s.Ref != nil {
		b, err := json.Marshal(s.Ref)
		if err != nil {
			return err
		}
		ref = string(b)
	}
	_, err := exec(ctx, db, `INSERT INTO sessions(id, agent_id, job_id, state, ref, live, last_seq, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state = excluded.state, ref = excluded.ref, live = excluded.live,
			last_seq = excluded.last_seq, updated_at = excluded.updated_at`,
		s.ID, s.AgentID, s.JobID, s.State, ref, b2i(s.Live), s.LastSeq, ms(s.UpdatedAt))
	return err
}

// ListSessionsOnMachine returns the sessions of the agents of a machine.
func ListSessionsOnMachine(ctx context.Context, db Querier, machineID ID) ([]Session, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+sessionCols+` FROM sessions s JOIN agents a ON a.id = s.agent_id
		WHERE a.machine_id = ? ORDER BY s.id`, machineID)
	return collect(rows, err, scanSession)
}

func scanSession(sc scanner) (Session, error) {
	var s Session
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

// AddSessionEvent stores a reliable IR event. It reports false for a seq
// that is already stored (a resend after reconnect).
func AddSessionEvent(ctx context.Context, db Executor, sessionID ID, e ir.Event) (bool, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	res, err := exec(ctx, db, `INSERT INTO session_events(session_id, seq, type, payload) VALUES (?, ?, ?, ?)
		ON CONFLICT(session_id, seq) DO NOTHING`, sessionID, e.Seq, e.Type, string(b))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ListSessionEvents returns the events of a session with seq > after,
// oldest first.
func ListSessionEvents(ctx context.Context, db Querier, sessionID ID, after int64, limit int) ([]ir.Event, error) {
	rows, err := db.QueryContext(ctx, `SELECT payload FROM session_events WHERE session_id = ? AND seq > ? ORDER BY seq LIMIT ?`,
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
