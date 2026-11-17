package model

import (
	"context"
	"time"
)

// JobState is the state of a job.
type JobState string

const (
	JobQueued    JobState = "queued"  // waits for its session to take the prompt
	JobRunning   JobState = "running" // the agent works on it
	JobDone      JobState = "done"    // the last turn completed; Result holds the answer
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

// Terminal reports whether no more work happens without a new input.
func (s JobState) Terminal() bool { return s == JobDone || s == JobFailed || s == JobCancelled }

// Job is one piece of work for one agent. It runs in its own session. The
// caller gets the job ID at once and does not wait: when the turn of the
// agent completes, the job is done and the result goes back to whoever
// asked (the Super Agent inbox, or the person).
type Job struct {
	ID          ID
	OwnerID     ID
	AgentID     ID
	SessionID   ID
	RequestedBy AuthorKind // human or super
	Title       string
	Prompt      string
	State       JobState
	Result      string // the last agent message of the last turn
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const jobCols = `id, owner_id, agent_id, session_id, requested_by, title, prompt, state, result, error, created_at, updated_at`

// GetJob returns the job with this ID.
func GetJob(ctx context.Context, db Querier, id ID) (Job, error) {
	return scanJob(db.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id = ?`, id))
}

// SaveJob inserts or updates a job by ID.
func SaveJob(ctx context.Context, db Executor, j Job) error {
	_, err := exec(ctx, db, `INSERT INTO jobs(`+jobCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state = excluded.state, result = excluded.result, error = excluded.error,
			title = excluded.title, updated_at = excluded.updated_at`,
		j.ID, j.OwnerID, j.AgentID, j.SessionID, j.RequestedBy, j.Title, j.Prompt, j.State, j.Result, j.Error,
		ms(j.CreatedAt), ms(j.UpdatedAt))
	return err
}

// ListJobs returns the jobs of an owner, newest first.
func ListJobs(ctx context.Context, db Querier, ownerID ID, limit int) ([]Job, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE owner_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		ownerID, limitOr(limit))
	return collect(rows, err, scanJob)
}

func scanJob(s scanner) (Job, error) {
	var j Job
	var by, state string
	var created, updated int64
	if err := s.Scan(&j.ID, &j.OwnerID, &j.AgentID, &j.SessionID, &by, &j.Title, &j.Prompt, &state, &j.Result, &j.Error, &created, &updated); err != nil {
		return j, mapErr(err)
	}
	j.RequestedBy, j.State, j.CreatedAt, j.UpdatedAt = AuthorKind(by), JobState(state), tm(created), tm(updated)
	return j, nil
}
