package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Dispatch gives a job to an agent and returns at once. The job runs in a
// new session of the agent. A person or the Super Agent may dispatch.
func (e *Engine) Dispatch(ctx context.Context, a Actor, agentRef, prompt string) (Job, error) {
	if err := requireManager(a); err != nil {
		return Job{}, err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Job{}, fmt.Errorf("%w: empty prompt", errs.ErrInvalid)
	}
	var j Job
	err := e.store.Atomic(ctx, func(tx Tx) error {
		ag, err := resolveAgentRef(ctx, tx, a, agentRef)
		if err != nil {
			return err
		}
		if ag.Role == RoleSuper {
			return fmt.Errorf("%w: %q is the Super Agent; send it a message instead", errs.ErrInvalid, ag.Name)
		}
		now := e.clock()
		j = Job{
			ID: NewIDAt(PrefixJob, now), OwnerID: a.OwnerID, AgentID: ag.ID, SessionID: NewIDAt(PrefixSession, now),
			RequestedBy: a.Kind, Title: clip(firstLine(prompt), 80), Prompt: prompt, State: JobQueued,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.SaveSession(ctx, Session{ID: j.SessionID, AgentID: ag.ID, JobID: j.ID, State: SessionStopped, UpdatedAt: now}); err != nil {
			return err
		}
		if err := tx.SaveJob(ctx, j); err != nil {
			return err
		}
		return tx.AddInput(ctx, Input{ID: NewIDAt(PrefixInput, now), SessionID: j.SessionID, Text: prompt, CreatedAt: now})
	})
	if err != nil {
		return Job{}, err
	}
	e.publishJob(ctx, j)
	e.wake()
	return j, nil
}

// FollowUp sends more input to the session of a job. A finished job runs
// again; its next turn gives the new result.
func (e *Engine) FollowUp(ctx context.Context, a Actor, jobID ID, text string) (Job, error) {
	if err := requireManager(a); err != nil {
		return Job{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Job{}, fmt.Errorf("%w: empty text", errs.ErrInvalid)
	}
	var j Job
	err := e.store.Atomic(ctx, func(tx Tx) error {
		var err error
		if j, err = ownedJob(ctx, tx, a, jobID); err != nil {
			return err
		}
		if j.State == JobCancelled {
			return fmt.Errorf("%w: job %s is cancelled", errs.ErrConflict, j.ID)
		}
		now := e.clock()
		if j.State.Terminal() {
			j.State, j.Error = JobQueued, ""
		}
		j.UpdatedAt = now
		if err := tx.SaveJob(ctx, j); err != nil {
			return err
		}
		return tx.AddInput(ctx, Input{ID: NewIDAt(PrefixInput, now), SessionID: j.SessionID, Text: text, CreatedAt: now})
	})
	if err != nil {
		return Job{}, err
	}
	e.publishJob(ctx, j)
	e.wake()
	return j, nil
}

// CancelJob stops a job. Pending inputs are dropped and the session is
// stopped if it runs.
func (e *Engine) CancelJob(ctx context.Context, a Actor, jobID ID) (Job, error) {
	if err := requireManager(a); err != nil {
		return Job{}, err
	}
	var (
		j       Job
		machine ID
		live    bool
	)
	err := e.store.Atomic(ctx, func(tx Tx) error {
		var err error
		if j, err = ownedJob(ctx, tx, a, jobID); err != nil {
			return err
		}
		if j.State.Terminal() && j.State != JobDone {
			return nil
		}
		pending, err := tx.PendingInputs(ctx, j.SessionID)
		if err != nil {
			return err
		}
		ids := make([]ID, len(pending))
		for i, in := range pending {
			ids[i] = in.ID
		}
		if err := tx.MarkInputsSent(ctx, ids); err != nil {
			return err
		}
		j.State, j.UpdatedAt = JobCancelled, e.clock()
		if err := tx.SaveJob(ctx, j); err != nil {
			return err
		}
		ses, err := tx.Session(ctx, j.SessionID)
		if err != nil {
			return err
		}
		ag, err := tx.Agent(ctx, j.AgentID)
		if err != nil {
			return err
		}
		machine, live = ag.MachineID, ses.Live
		return nil
	})
	if err != nil {
		return Job{}, err
	}
	if live && e.dispatch != nil && e.dispatch.Online(machine) {
		env := protocol.Envelope{Type: protocol.TypeSessionStop, Payload: mustJSON(protocol.SessionStop{SessionID: string(j.SessionID), Reason: "job cancelled"})}
		_, _ = e.dispatch.Send(ctx, machine, env)
	}
	e.publishJob(ctx, j)
	return j, nil
}

// Jobs returns the newest jobs first.
func (e *Engine) Jobs(ctx context.Context, a Actor, limit int) ([]Job, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	var out []Job
	err := e.store.Read(ctx, func(tx Tx) error {
		var err error
		out, err = tx.Jobs(ctx, a.OwnerID, limit)
		return err
	})
	return out, err
}

// Job returns one job.
func (e *Engine) Job(ctx context.Context, a Actor, id ID) (Job, error) {
	if err := requireManager(a); err != nil {
		return Job{}, err
	}
	var j Job
	err := e.store.Read(ctx, func(tx Tx) error {
		var err error
		j, err = ownedJob(ctx, tx, a, id)
		return err
	})
	return j, err
}

// SessionEvents returns the stored IR events of a session (no deltas).
func (e *Engine) SessionEvents(ctx context.Context, a Actor, sessionID ID, after int64, limit int) ([]ir.Event, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	var out []ir.Event
	err := e.store.Read(ctx, func(tx Tx) error {
		ses, err := tx.Session(ctx, sessionID)
		if err != nil {
			return err
		}
		ag, err := tx.Agent(ctx, ses.AgentID)
		if err != nil {
			return err
		}
		m, err := tx.Machine(ctx, ag.MachineID)
		if err != nil {
			return err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return err
		}
		out, err = tx.SessionEvents(ctx, sessionID, after, limit)
		return err
	})
	return out, err
}

func ownedJob(ctx context.Context, tx Tx, a Actor, id ID) (Job, error) {
	j, err := tx.Job(ctx, id)
	if err != nil {
		return Job{}, fmt.Errorf("job %s: %w", id, err)
	}
	if err := owned(a, j.OwnerID); err != nil {
		return Job{}, fmt.Errorf("job %s: %w", id, err)
	}
	return j, nil
}

func (e *Engine) publishJob(ctx context.Context, j Job) {
	var name string
	_ = e.store.Read(ctx, func(tx Tx) error {
		ag, err := tx.Agent(ctx, j.AgentID)
		name = ag.Name
		return err
	})
	e.publish(ctx, j.OwnerID, PushJob, WireJob(j, name))
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
