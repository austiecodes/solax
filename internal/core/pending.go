package core

import (
	"context"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
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

// SendToSuper appends a user message to the Super Agent conversation. (D)
func (e *Engine) SendToSuper(ctx context.Context, a Actor, text string) (Message, error) {
	if err := requireHuman(a); err != nil {
		return Message{}, err
	}
	return Message{}, errs.ErrNotImplemented
}

// SuperReply appends a Super Agent reply. (D)
func (e *Engine) SuperReply(ctx context.Context, a Actor, text string) (Message, error) {
	if a.Kind != AuthorSuper {
		return Message{}, errs.ErrForbidden
	}
	return Message{}, errs.ErrNotImplemented
}

// SessionStateChanged records a session state and flushes its batch. (A4)
func (e *Engine) SessionStateChanged(ctx context.Context, machineID ID, r protocol.SessionStateReport) error {
	return errs.ErrNotImplemented
}

// SessionEvent handles one IR event from a session. (A4)
func (e *Engine) SessionEvent(ctx context.Context, machineID ID, ev protocol.SessionEvent) error {
	return errs.ErrNotImplemented
}
