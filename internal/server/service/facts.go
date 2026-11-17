package service

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

// What daemons report through the hub: machines that connect, heartbeats,
// and session states and events. The names are in the past tense.

// MachineConnected records that the machine is online with the runtimes
// that its daemon detected. Sessions that the daemon still runs stay live;
// all other sessions of the machine are stopped, and the delivery loop
// starts them again when they have input.
func MachineConnected(ctx context.Context, machineID model.ID, h protocol.Hello) error {
	var owner model.ID
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		m, err := model.GetMachine(ctx, tx, machineID)
		if err != nil {
			return err
		}
		if m.State != model.MachineApproved {
			return errs.ErrForbidden
		}
		m.Online, m.LastSeen, m.Slots, m.Runtimes = true, clock(), h.FreeSlots, h.Runtimes
		owner = m.OwnerID
		if err := model.SaveMachine(ctx, tx, m); err != nil {
			return err
		}
		sessions, err := model.ListSessionsOnMachine(ctx, tx, machineID)
		if err != nil {
			return err
		}
		for _, ses := range sessions {
			state, live := h.Sessions[string(ses.ID)]
			if live == ses.Live && (!live || state == ses.State) {
				continue
			}
			ses.Live, ses.UpdatedAt = live, clock()
			if live {
				ses.State = state
			} else {
				ses.State = model.SessionStopped
			}
			if err := model.SaveSession(ctx, tx, ses); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		Publish(ctx, owner, PushMachines, nil)
		Wake()
	}
	return err
}

// MachineDisconnected records that the machine is offline.
func MachineDisconnected(ctx context.Context, machineID model.ID) error {
	var owner model.ID
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		m, err := model.GetMachine(ctx, tx, machineID)
		if err != nil {
			return err
		}
		owner = m.OwnerID
		m.Online, m.LastSeen = false, clock()
		return model.SaveMachine(ctx, tx, m)
	})
	if err == nil {
		Publish(ctx, owner, PushMachines, nil)
	}
	return err
}

// Heartbeat records the free slots of a machine. Session states in the
// heartbeat are reconciled by the delivery loop.
func Heartbeat(ctx context.Context, machineID model.ID, hb protocol.Heartbeat) error {
	return model.Atomic(ctx, func(tx *sql.Tx) error {
		m, err := model.GetMachine(ctx, tx, machineID)
		if err != nil {
			return err
		}
		m.Online, m.LastSeen, m.Slots = true, clock(), hb.FreeSlots
		return model.SaveMachine(ctx, tx, m)
	})
}

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

// SessionEvent stores a reliable IR event, moves the session and its job,
// and pushes the event to the clients. Deltas are only pushed.
func SessionEvent(ctx context.Context, machineID model.ID, se protocol.SessionEvent) error {
	ev := se.Event
	var (
		owner   model.ID
		note    protocol.SessionEventNote
		job     *model.Job
		agentNm string
		fresh   = true
		post    afterCommit
	)
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		post, job = nil, nil
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
		note = protocol.SessionEventNote{SessionID: string(ses.ID), Agent: ag.Name, JobID: string(ses.JobID), Super: ag.Role == model.RoleSuper, Event: ev}
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
		if ses.JobID == "" {
			return superEvent(ctx, tx, ses, ev, &post)
		}
		j, err := model.GetJob(ctx, tx, ses.JobID)
		if err != nil {
			return err
		}
		if changed := applyJobEvent(&j, ev); !changed {
			return nil
		}
		j.UpdatedAt = now
		if err := model.SaveJob(ctx, tx, j); err != nil {
			return err
		}
		job = &j
		if j.State.Terminal() {
			return jobFinished(ctx, tx, j, ag, &post)
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
	if job != nil {
		Publish(ctx, owner, PushJob, model.WireJob(*job, agentNm))
		if job.State.Terminal() {
			Wake()
		}
	}
	return nil
}

// applyJobEvent moves a job with one event of its session. It reports
// whether the job changed. The result is the last agent message of the
// last turn; a cancelled job stays cancelled.
func applyJobEvent(j *model.Job, ev ir.Event) bool {
	if j.State == model.JobCancelled {
		return false
	}
	switch ev.Type {
	case ir.TurnStarted:
		j.State, j.Result, j.Error = model.JobRunning, "", ""
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
			j.State = model.JobDone
		case ir.TurnInterrupted:
			j.State, j.Error = model.JobFailed, "interrupted"
		default:
			j.State, j.Error = model.JobFailed, "turn failed"
			if ev.Turn.Error != nil && ev.Turn.Error.Message != "" {
				j.Error = ev.Turn.Error.Message
			}
		}
		return true
	case ir.SessionExitedEvent:
		if !j.State.Terminal() {
			j.State, j.Error = model.JobFailed, "agent process exited"
			if ev.Session != nil && ev.Session.Reason != "" {
				j.Error += ": " + ev.Session.Reason
			}
			return true
		}
	}
	return false
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
