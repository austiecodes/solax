package model

import (
	"context"
	"time"
)

// TaskState is the state of a task.
type TaskState string

const (
	TaskQueued    TaskState = "queued"  // waits for its session to take the prompt
	TaskRunning   TaskState = "running" // the agent works on it
	TaskDone      TaskState = "done"    // the last turn completed; Result holds the answer
	TaskFailed    TaskState = "failed"
	TaskCancelled TaskState = "cancelled"
)

// Terminal reports whether no more work happens without a new input.
func (s TaskState) Terminal() bool { return s == TaskDone || s == TaskFailed || s == TaskCancelled }

// Task is one piece of work for one agent. It runs in its own session. The
// caller gets the task ID at once and does not wait: when the turn of the
// agent completes, the task is done and the result goes back to whoever
// asked (the Super Agent inbox, or the person).
type Task struct {
	ID      ID
	OwnerID ID
	AgentID ID
	// ProjectID is the project the task works in. Only an old task, from
	// before projects, can have none.
	ProjectID   ID
	SessionID   ID
	RequestedBy AuthorKind // who created the task: human or super; never changes
	// TurnBy is who started the current or queued turn. The result of the
	// turn goes to them: a finished turn of the Super Agent wakes it, a
	// finished turn of a person does not.
	TurnBy AuthorKind
	// WorkflowID is the workflow that started the task, if any. The
	// workflow waits for the turns of its tasks.
	WorkflowID ID
	Title      string
	Prompt     string
	State      TaskState
	Result     string // latest text while running, final answer when done
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const taskCols = `id, owner_id, agent_id, project_id, session_id, requested_by, turn_by, workflow_id, title, prompt, state, result, error, created_at, updated_at`

// GetTask returns the task with this ID.
func GetTask(ctx context.Context, db Querier, id ID) (Task, error) {
	return scanTask(db.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
}

// SaveTask inserts or updates a task by ID.
func SaveTask(ctx context.Context, db Executor, j Task) error {
	_, err := exec(ctx, db, `INSERT INTO tasks(`+taskCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET turn_by = excluded.turn_by, state = excluded.state, result = excluded.result, error = excluded.error,
			title = excluded.title, updated_at = excluded.updated_at`,
		j.ID, j.OwnerID, j.AgentID, j.ProjectID, j.SessionID, j.RequestedBy, j.TurnBy, j.WorkflowID, j.Title, j.Prompt, j.State, j.Result, j.Error,
		ms(j.CreatedAt), ms(j.UpdatedAt))
	return err
}

// ListTasks returns the tasks of an owner, newest first.
func ListTasks(ctx context.Context, db Querier, ownerID ID, limit int) ([]Task, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE owner_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		ownerID, limitOr(limit))
	return collect(rows, err, scanTask)
}

func scanTask(s scanner) (Task, error) {
	var j Task
	var by, turn, state string
	var created, updated int64
	if err := s.Scan(&j.ID, &j.OwnerID, &j.AgentID, &j.ProjectID, &j.SessionID, &by, &turn, &j.WorkflowID, &j.Title, &j.Prompt, &state, &j.Result, &j.Error, &created, &updated); err != nil {
		return j, mapErr(err)
	}
	j.RequestedBy, j.TurnBy, j.State, j.CreatedAt, j.UpdatedAt = AuthorKind(by), AuthorKind(turn), TaskState(state), tm(created), tm(updated)
	return j, nil
}

// ListWorkflowTasks returns the tasks that a workflow started, oldest
// first.
func ListWorkflowTasks(ctx context.Context, db Querier, workflowID ID) ([]Task, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE workflow_id = ? ORDER BY created_at, id`, workflowID)
	return collect(rows, err, scanTask)
}
