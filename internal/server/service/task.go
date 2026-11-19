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
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return model.Task{}, fmt.Errorf("%w: empty prompt", errs.ErrInvalid)
	}
	var j model.Task
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		ag, err := resolveAgentRef(ctx, tx, a, agentRef)
		if err != nil {
			return err
		}
		if ag.Role == model.RoleSuper {
			return fmt.Errorf("%w: %q is the Super Agent; send it a message instead", errs.ErrInvalid, ag.Name)
		}
		now := clock()
		j = model.Task{
			ID: model.NewIDAt(model.PrefixTask, now), OwnerID: a.OwnerID, AgentID: ag.ID, SessionID: model.NewIDAt(model.PrefixSession, now),
			RequestedBy: a.Kind, Title: clip(firstLine(prompt), 80), Prompt: prompt, State: model.TaskQueued,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := model.SaveSession(ctx, tx, model.Session{ID: j.SessionID, AgentID: ag.ID, TaskID: j.ID, State: model.SessionStopped, UpdatedAt: now}); err != nil {
			return err
		}
		if err := model.SaveTask(ctx, tx, j); err != nil {
			return err
		}
		return model.AddInput(ctx, tx, model.Input{ID: model.NewIDAt(model.PrefixInput, now), SessionID: j.SessionID, Text: prompt, CreatedAt: now})
	})
	if err != nil {
		return model.Task{}, err
	}
	publishTask(ctx, j)
	Wake()
	return j, nil
}

// FollowUp sends more input to the session of a task. A finished task runs
// again; its next turn gives the new result.
func FollowUp(ctx context.Context, a model.Actor, taskID model.ID, text string) (model.Task, error) {
	if err := requireManager(a); err != nil {
		return model.Task{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return model.Task{}, fmt.Errorf("%w: empty text", errs.ErrInvalid)
	}
	var j model.Task
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		var err error
		if j, err = ownedTask(ctx, tx, a, taskID); err != nil {
			return err
		}
		if j.State == model.TaskCancelled {
			return fmt.Errorf("%w: task %s is cancelled", errs.ErrConflict, j.ID)
		}
		now := clock()
		if j.State.Terminal() {
			j.State, j.Error = model.TaskQueued, ""
		}
		j.UpdatedAt = now
		if err := model.SaveTask(ctx, tx, j); err != nil {
			return err
		}
		return model.AddInput(ctx, tx, model.Input{ID: model.NewIDAt(model.PrefixInput, now), SessionID: j.SessionID, Text: text, CreatedAt: now})
	})
	if err != nil {
		return model.Task{}, err
	}
	publishTask(ctx, j)
	Wake()
	return j, nil
}

// CancelTask stops a task. Pending inputs are dropped and the session is
// stopped if it runs.
func CancelTask(ctx context.Context, a model.Actor, taskID model.ID) (model.Task, error) {
	if err := requireManager(a); err != nil {
		return model.Task{}, err
	}
	var (
		j       model.Task
		machine model.ID
		live    bool
	)
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		var err error
		if j, err = ownedTask(ctx, tx, a, taskID); err != nil {
			return err
		}
		if j.State.Terminal() && j.State != model.TaskDone {
			return nil
		}
		pending, err := model.ListPendingInputs(ctx, tx, j.SessionID)
		if err != nil {
			return err
		}
		ids := make([]model.ID, len(pending))
		for i, in := range pending {
			ids[i] = in.ID
		}
		if err := model.MarkInputsSent(ctx, tx, ids); err != nil {
			return err
		}
		j.State, j.UpdatedAt = model.TaskCancelled, clock()
		if err := model.SaveTask(ctx, tx, j); err != nil {
			return err
		}
		ses, err := model.GetSession(ctx, tx, j.SessionID)
		if err != nil {
			return err
		}
		ag, err := model.GetAgent(ctx, tx, j.AgentID)
		if err != nil {
			return err
		}
		machine, live = ag.MachineID, ses.Live
		return nil
	})
	if err != nil {
		return model.Task{}, err
	}
	if live && Online(machine) {
		env := protocol.Envelope{Type: protocol.TypeSessionStop, Payload: mustJSON(protocol.SessionStop{SessionID: string(j.SessionID), Reason: "task cancelled"})}
		_, _ = Send(ctx, machine, env)
	}
	publishTask(ctx, j)
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
// whether the task changed. The result is the last agent message of the
// last turn; a cancelled task stays cancelled.
func applyTaskEvent(j *model.Task, ev ir.Event) bool {
	if j.State == model.TaskCancelled {
		return false
	}
	switch ev.Type {
	case ir.TurnStarted:
		j.State, j.Result, j.Error = model.TaskRunning, "", ""
		return true
	case ir.ItemCompleted:
		if ev.Item != nil && ev.Item.Kind == ir.ItemAgentMessage && strings.TrimSpace(ev.Item.Text) != "" {
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
		case ir.TurnInterrupted:
			j.State, j.Error = model.TaskFailed, "interrupted"
		default:
			j.State, j.Error = model.TaskFailed, "turn failed"
			if ev.Turn.Error != nil && ev.Turn.Error.Message != "" {
				j.Error = ev.Turn.Error.Message
			}
		}
		return true
	case ir.SessionExitedEvent:
		if !j.State.Terminal() {
			j.State, j.Error = model.TaskFailed, "agent process exited"
			if ev.Session != nil && ev.Session.Reason != "" {
				j.Error += ": " + ev.Session.Reason
			}
			return true
		}
	}
	return false
}
