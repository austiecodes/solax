package model

import (
	"context"
	"time"
)

// WorkflowState is the state of a workflow.
type WorkflowState string

const (
	WorkflowRunning   WorkflowState = "running"
	WorkflowWaiting   WorkflowState = "waiting" // wf.ask waits for an answer
	WorkflowDone      WorkflowState = "done"
	WorkflowFailed    WorkflowState = "failed"
	WorkflowCancelled WorkflowState = "cancelled"
)

// Active reports whether the script still runs or waits.
func (s WorkflowState) Active() bool { return s == WorkflowRunning || s == WorkflowWaiting }

// Workflow is one run of a workflow script. The server runs it; each wait
// of the script is a WorkflowStep.
type Workflow struct {
	ID          ID
	OwnerID     ID
	RequestedBy AuthorKind // human or super; it hears when the workflow ends
	Name        string     // meta.name, or the saved workflow it ran from
	Title       string
	Source      string // TypeScript
	ArgsJSON    string
	ResumeFrom  ID // a run whose finished steps this run may reuse
	State       WorkflowState
	Result      string // JSON
	Error       string
	Logs        string // wf.log lines, newest last
	LogSeq      int    // seq of the last stored log line
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const workflowCols = `id, owner_id, requested_by, name, title, source, args_json, resume_from, state, result, error, logs, log_seq, created_at, updated_at`

// SaveWorkflow inserts or updates a workflow by ID.
func SaveWorkflow(ctx context.Context, db Executor, w Workflow) error {
	_, err := exec(ctx, db, `INSERT INTO workflows(`+workflowCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state = excluded.state, result = excluded.result, error = excluded.error,
			logs = excluded.logs, log_seq = excluded.log_seq, updated_at = excluded.updated_at`,
		w.ID, w.OwnerID, w.RequestedBy, w.Name, w.Title, w.Source, w.ArgsJSON, w.ResumeFrom, w.State, w.Result, w.Error,
		w.Logs, w.LogSeq, ms(w.CreatedAt), ms(w.UpdatedAt))
	return err
}

// GetWorkflow returns the workflow with this ID.
func GetWorkflow(ctx context.Context, db Querier, id ID) (Workflow, error) {
	return scanWorkflow(db.QueryRowContext(ctx, `SELECT `+workflowCols+` FROM workflows WHERE id = ?`, id))
}

// ListWorkflows returns the workflows of an owner, newest first.
func ListWorkflows(ctx context.Context, db Querier, ownerID ID, limit int) ([]Workflow, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+workflowCols+` FROM workflows WHERE owner_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		ownerID, limitOr(limit))
	return collect(rows, err, scanWorkflow)
}

// ListActiveWorkflows returns the workflows of all owners that run or
// wait, oldest first. The server resumes them when it starts.
func ListActiveWorkflows(ctx context.Context, db Querier) ([]Workflow, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+workflowCols+` FROM workflows WHERE state IN ('running', 'waiting') ORDER BY created_at, id`)
	return collect(rows, err, scanWorkflow)
}

func scanWorkflow(s scanner) (Workflow, error) {
	var w Workflow
	var by, state string
	var created, updated int64
	if err := s.Scan(&w.ID, &w.OwnerID, &by, &w.Name, &w.Title, &w.Source, &w.ArgsJSON, &w.ResumeFrom, &state,
		&w.Result, &w.Error, &w.Logs, &w.LogSeq, &created, &updated); err != nil {
		return w, mapErr(err)
	}
	w.RequestedBy, w.State, w.CreatedAt, w.UpdatedAt = AuthorKind(by), WorkflowState(state), tm(created), tm(updated)
	return w, nil
}

// StepState is the state of a workflow step.
type StepState string

const (
	StepPending StepState = "pending"
	StepDone    StepState = "done"
	StepFailed  StepState = "failed" // OutputJSON holds the error text as a JSON string
)

// WorkflowStep is one wait of a workflow script: a task turn, an answer, a
// sleep or a read. Its output is saved, so a script that runs again gets it
// without doing the work again.
type WorkflowStep struct {
	WorkflowID  ID
	Key         string
	Seq         int
	Kind        string
	Fingerprint string
	TargetID    string // tsk_ ID, or the wake time of a sleep
	Phase       string
	Label       string
	State       StepState
	OutputJSON  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const stepCols = `workflow_id, step_key, seq, kind, fingerprint, target_id, phase, label, state, output_json, created_at, updated_at`

// SaveStep inserts or updates a step by workflow and key.
func SaveStep(ctx context.Context, db Executor, s WorkflowStep) error {
	_, err := exec(ctx, db, `INSERT INTO workflow_steps(`+stepCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workflow_id, step_key) DO UPDATE SET target_id = excluded.target_id, state = excluded.state,
			output_json = excluded.output_json, updated_at = excluded.updated_at`,
		s.WorkflowID, s.Key, s.Seq, s.Kind, s.Fingerprint, s.TargetID, s.Phase, s.Label, s.State, s.OutputJSON,
		ms(s.CreatedAt), ms(s.UpdatedAt))
	return err
}

// GetStep returns one step of a workflow.
func GetStep(ctx context.Context, db Querier, workflowID ID, key string) (WorkflowStep, error) {
	return scanStep(db.QueryRowContext(ctx, `SELECT `+stepCols+` FROM workflow_steps WHERE workflow_id = ? AND step_key = ?`, workflowID, key))
}

// ListSteps returns the steps of a workflow in call order.
func ListSteps(ctx context.Context, db Querier, workflowID ID) ([]WorkflowStep, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+stepCols+` FROM workflow_steps WHERE workflow_id = ? ORDER BY seq, step_key`, workflowID)
	return collect(rows, err, scanStep)
}

func scanStep(sc scanner) (WorkflowStep, error) {
	var s WorkflowStep
	var state string
	var created, updated int64
	if err := sc.Scan(&s.WorkflowID, &s.Key, &s.Seq, &s.Kind, &s.Fingerprint, &s.TargetID, &s.Phase, &s.Label, &state,
		&s.OutputJSON, &created, &updated); err != nil {
		return s, mapErr(err)
	}
	s.State, s.CreatedAt, s.UpdatedAt = StepState(state), tm(created), tm(updated)
	return s, nil
}

// SavedWorkflow is a workflow script kept under a name, to run again.
type SavedWorkflow struct {
	OwnerID     ID
	Name        string
	Description string
	MetaJSON    string
	Source      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const savedCols = `owner_id, name, description, meta_json, source, created_at, updated_at`

// SaveSavedWorkflow inserts a saved workflow, or replaces the one with the
// same name.
func SaveSavedWorkflow(ctx context.Context, db Executor, s SavedWorkflow) error {
	_, err := exec(ctx, db, `INSERT INTO saved_workflows(`+savedCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(owner_id, name) DO UPDATE SET description = excluded.description, meta_json = excluded.meta_json,
			source = excluded.source, updated_at = excluded.updated_at`,
		s.OwnerID, s.Name, s.Description, s.MetaJSON, s.Source, ms(s.CreatedAt), ms(s.UpdatedAt))
	return err
}

// GetSavedWorkflow returns the saved workflow of an owner with this name.
func GetSavedWorkflow(ctx context.Context, db Querier, ownerID ID, name string) (SavedWorkflow, error) {
	return scanSaved(db.QueryRowContext(ctx, `SELECT `+savedCols+` FROM saved_workflows WHERE owner_id = ? AND name = ?`, ownerID, name))
}

// ListSavedWorkflows returns the saved workflows of an owner by name.
func ListSavedWorkflows(ctx context.Context, db Querier, ownerID ID) ([]SavedWorkflow, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+savedCols+` FROM saved_workflows WHERE owner_id = ? ORDER BY name`, ownerID)
	return collect(rows, err, scanSaved)
}

func scanSaved(sc scanner) (SavedWorkflow, error) {
	var s SavedWorkflow
	var created, updated int64
	if err := sc.Scan(&s.OwnerID, &s.Name, &s.Description, &s.MetaJSON, &s.Source, &created, &updated); err != nil {
		return s, mapErr(err)
	}
	s.CreatedAt, s.UpdatedAt = tm(created), tm(updated)
	return s, nil
}
