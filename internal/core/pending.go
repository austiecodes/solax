package core

import (
	"context"

	"github.com/austiecodes/solax/internal/errs"
)

// Commands that later roadmap steps implement (docs/roadmap.md §3).
// Each one keeps its authorization check, so the API is already safe.

// CreateTask creates a task, appends task.started and starts its
// orchestrator. (A3)
func (e *Engine) CreateTask(ctx context.Context, a Actor, ws ID, brief string) (Task, error) {
	if err := requireManager(a); err != nil {
		return Task{}, err
	}
	return Task{}, errs.ErrNotImplemented
}

// CancelTask cancels a task and its subtree. (A3)
func (e *Engine) CancelTask(ctx context.Context, a Actor, task ID) error {
	if err := requireManager(a); err != nil {
		return err
	}
	return errs.ErrNotImplemented
}

// RequestSummary asks the task's orchestrator for a summary. (A3)
func (e *Engine) RequestSummary(ctx context.Context, a Actor, task ID) error {
	if err := requireManager(a); err != nil {
		return err
	}
	return errs.ErrNotImplemented
}

// PostMessage appends a message and its task event, wakes mentioned
// members and charges the budget. (A3)
func (e *Engine) PostMessage(ctx context.Context, a Actor, ws ID, in PostInput) (Message, error) {
	if err := checkScope(a, ws, in.Task); err != nil {
		return Message{}, err
	}
	return Message{}, errs.ErrNotImplemented
}
