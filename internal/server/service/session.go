package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Session states and events, as daemons report them through the hub, and
// the stored events that clients read.

// SessionStateChanged records what the daemon says about a session
// process: running (with its native ref) or gone.
func SessionStateChanged(ctx context.Context, machineID model.ID, r protocol.SessionStateReport) error {
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		ses, err := sessionOn(ctx, tx, machineID, model.ID(r.SessionID))
		if err != nil {
			return err
		}
		ses.State, ses.UpdatedAt = r.State, clock()
		ses.Live = r.State != ir.SessionExited && r.State != model.SessionStopped
		if !ses.Live {
			ses.State = model.SessionStopped
		}
		if r.Ref != nil {
			ses.Ref = r.Ref
		}
		return model.SaveSession(ctx, tx, ses)
	})
	if err == nil {
		Wake()
	}
	return err
}

// SessionEvent stores a reliable IR event, moves the session and its task,
// and pushes the event to the clients. Deltas are only pushed.
func SessionEvent(ctx context.Context, machineID model.ID, se protocol.SessionEvent) error {
	ev := se.Event
	var (
		owner   model.ID
		note    protocol.SessionEventNote
		task    *model.Task
		agentNm string
		fresh   = true
		post    afterCommit
	)
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		post, task = nil, nil
		ses, err := sessionOn(ctx, tx, machineID, model.ID(ev.SessionID))
		if err != nil {
			return err
		}
		ag, err := model.GetAgent(ctx, tx, ses.AgentID)
		if err != nil {
			return err
		}
		m, err := model.GetMachine(ctx, tx, machineID)
		if err != nil {
			return err
		}
		owner, agentNm = m.OwnerID, ag.Name
		note = protocol.SessionEventNote{SessionID: string(ses.ID), Agent: ag.Name, TaskID: string(ses.TaskID), Super: ag.Role == model.RoleSuper, Event: ev}
		if !ev.Type.Reliable() {
			return nil
		}
		if fresh, err = model.AddSessionEvent(ctx, tx, ses.ID, ev); err != nil || !fresh {
			return err
		}
		now := clock()
		ses.LastSeq = max(ses.LastSeq, int64(ev.Seq))
		switch ev.Type {
		case ir.SessionStarted:
			ses.State, ses.Live = ir.SessionIdle, true
			if ev.Session != nil {
				ref := ev.Session.Ref
				ses.Ref = &ref
			}
		case ir.TurnStarted:
			ses.State = ir.SessionBusy
		case ir.TurnCompleted:
			ses.State = ir.SessionIdle
		case ir.SessionExitedEvent:
			ses.State, ses.Live = model.SessionStopped, false
		}
		ses.UpdatedAt = now
		if err := model.SaveSession(ctx, tx, ses); err != nil {
			return err
		}
		if ses.TaskID == "" {
			return superEvent(ctx, tx, ses, ev, &post)
		}
		j, err := model.GetTask(ctx, tx, ses.TaskID)
		if err != nil {
			return err
		}
		if changed := applyTaskEvent(&j, ev); !changed {
			return nil
		}
		j.UpdatedAt = now
		if err := model.SaveTask(ctx, tx, j); err != nil {
			return err
		}
		task = &j
		if j.State.Terminal() {
			return taskFinished(ctx, tx, j, ag, &post)
		}
		return nil
	})
	if err != nil {
		return err
	}
	post.run()
	if fresh {
		Publish(ctx, owner, PushSession, note)
	}
	if task != nil {
		Publish(ctx, owner, PushTask, model.WireTask(*task, agentNm))
		if task.State.Terminal() {
			Wake()
		}
	}
	return nil
}

// sessionOn returns a session that runs on the machine. A daemon cannot
// report events for sessions of another machine.
func sessionOn(ctx context.Context, db model.Querier, machineID, sessionID model.ID) (model.Session, error) {
	ses, err := model.GetSession(ctx, db, sessionID)
	if err != nil {
		return model.Session{}, fmt.Errorf("session %s: %w", sessionID, err)
	}
	ag, err := model.GetAgent(ctx, db, ses.AgentID)
	if err != nil {
		return model.Session{}, err
	}
	if ag.MachineID != machineID {
		return model.Session{}, fmt.Errorf("session %s: %w", sessionID, errs.ErrForbidden)
	}
	return ses, nil
}

// SessionEvents returns the stored IR events of a session (no deltas).
func SessionEvents(ctx context.Context, a model.Actor, sessionID model.ID, after int64, limit int) ([]ir.Event, error) {
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
