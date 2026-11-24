package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Dispatch gives a task to an agent and returns at once. The task runs in a
// new session of the agent. A person or the Super Agent may dispatch.
func Dispatch(ctx context.Context, a model.Actor, agentRef, prompt string) (model.Task, error) {
	if err := requireManager(a); err != nil {
		return model.Task{}, err
	}
	var j model.Task
	err := model.Atomic(ctx, func(tx *sql.Tx) (err error) {
		j, err = dispatch(ctx, tx, a, agentRef, prompt, "")
		return err
	})
	if err != nil {
		return model.Task{}, err
	}
	publishTask(ctx, j)
	Wake()
	return j, nil
}

// dispatch creates a task, its session and its first input. workflow is
// the workflow that starts the task, or "".
func dispatch(ctx context.Context, tx *sql.Tx, a model.Actor, agentRef, prompt string, workflow model.ID) (model.Task, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return model.Task{}, fmt.Errorf("%w: empty prompt", errs.ErrInvalid)
	}
	ag, err := resolveAgentRef(ctx, tx, a, agentRef)
	if err != nil {
		return model.Task{}, err
	}
	if ag.Role == model.RoleSuper {
		return model.Task{}, fmt.Errorf("%w: %q is the Super Agent; send it a message instead", errs.ErrInvalid, ag.Name)
	}
	now := clock()
	j := model.Task{
		ID: model.NewIDAt(model.PrefixTask, now), OwnerID: a.OwnerID, AgentID: ag.ID, SessionID: model.NewIDAt(model.PrefixSession, now),
		RequestedBy: a.Kind, TurnBy: a.Kind, WorkflowID: workflow, Title: clip(firstLine(prompt), 80), Prompt: prompt, State: model.TaskQueued,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := model.SaveSession(ctx, tx, model.Session{ID: j.SessionID, AgentID: ag.ID, TaskID: j.ID, State: model.SessionStopped, UpdatedAt: now}); err != nil {
		return model.Task{}, err
	}
	if err := model.SaveTask(ctx, tx, j); err != nil {
		return model.Task{}, err
	}
	return j, model.AddInput(ctx, tx, model.Input{ID: model.NewIDAt(model.PrefixInput, now), SessionID: j.SessionID, Text: prompt, CreatedAt: now})
}

// FollowUp sends more input to the session of a task. mode is
// ir.ModeFollowUp ("" too), which starts a new turn that the sender owns,
// or ir.ModeSteer, which guides the running turn and leaves its owner. A
// person may send input to any task (Fast Path); when the Super Agent has a
// stake in the task, it gets a passive note.
func FollowUp(ctx context.Context, a model.Actor, taskID model.ID, text string, mode ir.InputMode) (model.Task, error) {
	if err := requireManager(a); err != nil {
		return model.Task{}, err
	}
	var j model.Task
	err := model.Atomic(ctx, func(tx *sql.Tx) (err error) {
		j, err = followUp(ctx, tx, a, taskID, text, mode)
		return err
	})
	if err != nil {
		return model.Task{}, err
	}
	publishTask(ctx, j)
	Wake()
	return j, nil
}

func followUp(ctx context.Context, tx *sql.Tx, a model.Actor, taskID model.ID, text string, mode ir.InputMode) (model.Task, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return model.Task{}, fmt.Errorf("%w: empty text", errs.ErrInvalid)
	}
	switch mode {
	case "", ir.ModeFollowUp, ir.ModeSteer:
	default:
		return model.Task{}, fmt.Errorf("%w: mode %q: want follow_up or steer", errs.ErrInvalid, mode)
	}
	j, err := ownedTask(ctx, tx, a, taskID)
	if err != nil {
		return model.Task{}, err
	}
	if j.State == model.TaskCancelled {
		return model.Task{}, fmt.Errorf("%w: task %s is cancelled", errs.ErrConflict, j.ID)
	}
	mode = inputMode(j.State, mode)
	turnBy, note := turnAfterInput(j, a.Kind, mode)
	if note {
		ag, err := model.GetAgent(ctx, tx, j.AgentID)
		if err != nil {
			return model.Task{}, err
		}
		if err := superInput(ctx, tx, ir.ModeNote, inputNote(j, ag.Name, mode, text)); err != nil {
			return model.Task{}, err
		}
	}
	now := clock()
	if j.State.Terminal() {
		j.State, j.Error = model.TaskQueued, ""
	}
	j.TurnBy, j.UpdatedAt = turnBy, now
	if err := model.SaveTask(ctx, tx, j); err != nil {
		return model.Task{}, err
	}
	return j, model.AddInput(ctx, tx, model.Input{ID: model.NewIDAt(model.PrefixInput, now), SessionID: j.SessionID, Mode: mode, Text: text, CreatedAt: now})
}

// CancelTask stops a task. Pending inputs are dropped and the session is
// stopped if it runs.
func CancelTask(ctx context.Context, a model.Actor, taskID model.ID) (model.Task, error) {
	if err := requireManager(a); err != nil {
		return model.Task{}, err
	}
	var (
		j    model.Task
		post afterCommit
	)
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		post = nil
		var err error
		if j, err = ownedTask(ctx, tx, a, taskID); err != nil {
			return err
		}
		j, err = cancelTask(ctx, tx, a, j, &post)
		return err
	})
	if err != nil {
		return model.Task{}, err
	}
	post.run()
	return j, nil
}

// cancelTask cancels j in tx. After the commit, post stops its session,
// pushes the task, and wakes the workflow of the task.
func cancelTask(ctx context.Context, tx *sql.Tx, a model.Actor, j model.Task, post *afterCommit) (model.Task, error) {
	if j.State.Terminal() && j.State != model.TaskDone {
		return j, nil
	}
	pending, err := model.ListPendingInputs(ctx, tx, j.SessionID)
	if err != nil {
		return j, err
	}
	ids := make([]model.ID, len(pending))
	for i, in := range pending {
		ids[i] = in.ID
	}
	if err := model.MarkInputsSent(ctx, tx, ids); err != nil {
		return j, err
	}
	j.State, j.UpdatedAt = model.TaskCancelled, clock()
	if err := model.SaveTask(ctx, tx, j); err != nil {
		return j, err
	}
	ses, err := model.GetSession(ctx, tx, j.SessionID)
	if err != nil {
		return j, err
	}
	ag, err := model.GetAgent(ctx, tx, j.AgentID)
	if err != nil {
		return j, err
	}
	cancelled := j
	post.add(func() {
		ctx := context.WithoutCancel(ctx)
		if ses.Live && Online(ag.MachineID) {
			env := protocol.Envelope{Type: protocol.TypeSessionStop, Payload: mustJSON(protocol.SessionStop{SessionID: string(cancelled.SessionID), Reason: "task cancelled"})}
			_, _ = Send(ctx, ag.MachineID, env)
		}
		Publish(ctx, cancelled.OwnerID, PushTask, model.WireTask(cancelled, ag.Name))
		if cancelled.WorkflowID != "" {
			WakeWorkflow(cancelled.WorkflowID)
		}
	})
	if mode := cancelMode(j, a.Kind); mode != "" {
		return j, toSuper(ctx, tx, j.OwnerID, fmt.Sprintf("[task %s] agent %s: cancelled by the user", j.ID, ag.Name), "", mode, post)
	}
	return j, nil
}

// Tasks returns the newest tasks first.
func Tasks(ctx context.Context, a model.Actor, limit int) ([]model.Task, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	return model.ListTasks(ctx, model.ReadDB, a.OwnerID, limit)
}

// Task returns one task.
func Task(ctx context.Context, a model.Actor, id model.ID) (model.Task, error) {
	if err := requireManager(a); err != nil {
		return model.Task{}, err
	}
	return ownedTask(ctx, model.ReadDB, a, id)
}

func ownedTask(ctx context.Context, db model.Querier, a model.Actor, id model.ID) (model.Task, error) {
	j, err := model.GetTask(ctx, db, id)
	if err != nil {
		return model.Task{}, fmt.Errorf("task %s: %w", id, err)
	}
	if err := owned(a, j.OwnerID); err != nil {
		return model.Task{}, fmt.Errorf("task %s: %w", id, err)
	}
	return j, nil
}

func publishTask(ctx context.Context, j model.Task) {
	ag, _ := model.GetAgent(ctx, model.ReadDB, j.AgentID)
	Publish(ctx, j.OwnerID, PushTask, model.WireTask(j, ag.Name))
}

// applyTaskEvent moves a task with one event of its session. It reports
// whether the task changed. While running, Result shows a preview.
// On success, the daemon supplies the final answer.
func applyTaskEvent(j *model.Task, ev ir.Event) bool {
	if j.State == model.TaskCancelled {
		return false
	}
	switch ev.Type {
	case ir.TurnStarted:
		j.State, j.Result, j.Error = model.TaskRunning, "", ""
		return true
	case ir.ItemCompleted:
		if j.State == model.TaskRunning && ev.Item != nil && ev.Item.Kind == ir.ItemAgentMessage && strings.TrimSpace(ev.Item.Text) != "" {
			j.Result = ev.Item.Text
			return true
		}
	case ir.TurnCompleted:
		if ev.Turn == nil {
			return false
		}
		switch ev.Turn.Status {
		case ir.TurnOK:
			j.State = model.TaskDone
			if ev.Turn.Answer != nil {
				j.Result = *ev.Turn.Answer
			}
		case ir.TurnInterrupted:
			j.State, j.Result, j.Error = model.TaskFailed, "", "interrupted"
		default:
			j.State, j.Result, j.Error = model.TaskFailed, "", "turn failed"
			if ev.Turn.Error != nil && ev.Turn.Error.Message != "" {
				j.Error = ev.Turn.Error.Message
			}
		}
		return true
	case ir.SessionExitedEvent:
		if !j.State.Terminal() {
			j.State, j.Result, j.Error = model.TaskFailed, "", "agent process exited"
			if ev.Session != nil && ev.Session.Reason != "" {
				j.Error += ": " + ev.Session.Reason
			}
			return true
		}
	}
	return false
}
