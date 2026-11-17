package engine

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/server/model"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Dispatch gives a job to an agent and returns at once. The job runs in a
// new session of the agent. A person or the Super Agent may dispatch.
func (e *Engine) Dispatch(ctx context.Context, a model.Actor, agentRef, prompt string) (model.Job, error) {
	if err := requireManager(a); err != nil {
		return model.Job{}, err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return model.Job{}, fmt.Errorf("%w: empty prompt", errs.ErrInvalid)
	}
	var j model.Job
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		ag, err := resolveAgentRef(ctx, tx, a, agentRef)
		if err != nil {
			return err
		}
		if ag.Role == model.RoleSuper {
			return fmt.Errorf("%w: %q is the Super Agent; send it a message instead", errs.ErrInvalid, ag.Name)
		}
		now := e.clock()
		j = model.Job{
			ID: model.NewIDAt(model.PrefixJob, now), OwnerID: a.OwnerID, AgentID: ag.ID, SessionID: model.NewIDAt(model.PrefixSession, now),
			RequestedBy: a.Kind, Title: clip(firstLine(prompt), 80), Prompt: prompt, State: model.JobQueued,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := model.SaveSession(ctx, tx, model.Session{ID: j.SessionID, AgentID: ag.ID, JobID: j.ID, State: model.SessionStopped, UpdatedAt: now}); err != nil {
			return err
		}
		if err := model.SaveJob(ctx, tx, j); err != nil {
			return err
		}
		return model.AddInput(ctx, tx, model.Input{ID: model.NewIDAt(model.PrefixInput, now), SessionID: j.SessionID, Text: prompt, CreatedAt: now})
	})
	if err != nil {
		return model.Job{}, err
	}
	e.publishJob(ctx, j)
	e.wake()
	return j, nil
}

// FollowUp sends more input to the session of a job. A finished job runs
// again; its next turn gives the new result.
func (e *Engine) FollowUp(ctx context.Context, a model.Actor, jobID model.ID, text string) (model.Job, error) {
	if err := requireManager(a); err != nil {
		return model.Job{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return model.Job{}, fmt.Errorf("%w: empty text", errs.ErrInvalid)
	}
	var j model.Job
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		var err error
		if j, err = ownedJob(ctx, tx, a, jobID); err != nil {
			return err
		}
		if j.State == model.JobCancelled {
			return fmt.Errorf("%w: job %s is cancelled", errs.ErrConflict, j.ID)
		}
		now := e.clock()
		if j.State.Terminal() {
			j.State, j.Error = model.JobQueued, ""
		}
		j.UpdatedAt = now
		if err := model.SaveJob(ctx, tx, j); err != nil {
			return err
		}
		return model.AddInput(ctx, tx, model.Input{ID: model.NewIDAt(model.PrefixInput, now), SessionID: j.SessionID, Text: text, CreatedAt: now})
	})
	if err != nil {
		return model.Job{}, err
	}
	e.publishJob(ctx, j)
	e.wake()
	return j, nil
}

// CancelJob stops a job. Pending inputs are dropped and the session is
// stopped if it runs.
func (e *Engine) CancelJob(ctx context.Context, a model.Actor, jobID model.ID) (model.Job, error) {
	if err := requireManager(a); err != nil {
		return model.Job{}, err
	}
	var (
		j       model.Job
		machine model.ID
		live    bool
	)
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		var err error
		if j, err = ownedJob(ctx, tx, a, jobID); err != nil {
			return err
		}
		if j.State.Terminal() && j.State != model.JobDone {
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
		j.State, j.UpdatedAt = model.JobCancelled, e.clock()
		if err := model.SaveJob(ctx, tx, j); err != nil {
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
		return model.Job{}, err
	}
	if live && e.dispatch != nil && e.dispatch.Online(machine) {
		env := protocol.Envelope{Type: protocol.TypeSessionStop, Payload: mustJSON(protocol.SessionStop{SessionID: string(j.SessionID), Reason: "job cancelled"})}
		_, _ = e.dispatch.Send(ctx, machine, env)
	}
	e.publishJob(ctx, j)
	return j, nil
}

// Jobs returns the newest jobs first.
func (e *Engine) Jobs(ctx context.Context, a model.Actor, limit int) ([]model.Job, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	var out []model.Job
	err := model.Snapshot(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = model.ListJobs(ctx, tx, a.OwnerID, limit)
		return err
	})
	return out, err
}

// Job returns one job.
func (e *Engine) Job(ctx context.Context, a model.Actor, id model.ID) (model.Job, error) {
	if err := requireManager(a); err != nil {
		return model.Job{}, err
	}
	var j model.Job
	err := model.Snapshot(ctx, func(tx *sql.Tx) error {
		var err error
		j, err = ownedJob(ctx, tx, a, id)
		return err
	})
	return j, err
}

// SessionEvents returns the stored IR events of a session (no deltas).
func (e *Engine) SessionEvents(ctx context.Context, a model.Actor, sessionID model.ID, after int64, limit int) ([]ir.Event, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	var out []ir.Event
	err := model.Snapshot(ctx, func(tx *sql.Tx) error {
		ses, err := model.GetSession(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		ag, err := model.GetAgent(ctx, tx, ses.AgentID)
		if err != nil {
			return err
		}
		m, err := model.GetMachine(ctx, tx, ag.MachineID)
		if err != nil {
			return err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return err
		}
		out, err = model.ListSessionEvents(ctx, tx, sessionID, after, limit)
		return err
	})
	return out, err
}

func ownedJob(ctx context.Context, tx *sql.Tx, a model.Actor, id model.ID) (model.Job, error) {
	j, err := model.GetJob(ctx, tx, id)
	if err != nil {
		return model.Job{}, fmt.Errorf("job %s: %w", id, err)
	}
	if err := owned(a, j.OwnerID); err != nil {
		return model.Job{}, fmt.Errorf("job %s: %w", id, err)
	}
	return j, nil
}

func (e *Engine) publishJob(ctx context.Context, j model.Job) {
	var name string
	_ = model.Snapshot(ctx, func(tx *sql.Tx) error {
		ag, err := model.GetAgent(ctx, tx, j.AgentID)
		name = ag.Name
		return err
	})
	e.publish(ctx, j.OwnerID, PushJob, model.WireJob(j, name))
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
