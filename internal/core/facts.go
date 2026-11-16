package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// What daemons report through the hub: machines that connect, heartbeats,
// and session states and events. The names are in the past tense.

// MachineConnected records that the machine is online with the drivers
// that its daemon detected. Sessions that the daemon still runs stay live;
// all other sessions of the machine are stopped, and the delivery loop
// starts them again when they have input.
func (e *Engine) MachineConnected(ctx context.Context, machineID ID, h protocol.Hello) error {
	var owner ID
	err := e.store.Atomic(ctx, func(tx Tx) error {
		m, err := tx.Machine(ctx, machineID)
		if err != nil {
			return err
		}
		if m.State != MachineApproved {
			return errs.ErrForbidden
		}
		m.Online, m.LastSeen, m.Slots, m.Drivers = true, e.clock(), h.FreeSlots, h.Drivers
		owner = m.OwnerID
		if err := tx.SaveMachine(ctx, m); err != nil {
			return err
		}
		sessions, err := tx.SessionsOnMachine(ctx, machineID)
		if err != nil {
			return err
		}
		for _, ses := range sessions {
			state, live := h.Sessions[string(ses.ID)]
			if live == ses.Live && (!live || state == ses.State) {
				continue
			}
			ses.Live, ses.UpdatedAt = live, e.clock()
			if live {
				ses.State = state
			} else {
				ses.State = SessionStopped
			}
			if err := tx.SaveSession(ctx, ses); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		e.publish(ctx, owner, PushMachines, nil)
		e.wake()
	}
	return err
}

// MachineDisconnected records that the machine is offline.
func (e *Engine) MachineDisconnected(ctx context.Context, machineID ID) error {
	var owner ID
	err := e.store.Atomic(ctx, func(tx Tx) error {
		m, err := tx.Machine(ctx, machineID)
		if err != nil {
			return err
		}
		owner = m.OwnerID
		m.Online, m.LastSeen = false, e.clock()
		return tx.SaveMachine(ctx, m)
	})
	if err == nil {
		e.publish(ctx, owner, PushMachines, nil)
	}
	return err
}

// Heartbeat records the free slots of a machine. Session states in the
// heartbeat are reconciled by the delivery engine.
func (e *Engine) Heartbeat(ctx context.Context, machineID ID, hb protocol.Heartbeat) error {
	return e.store.Atomic(ctx, func(tx Tx) error {
		m, err := tx.Machine(ctx, machineID)
		if err != nil {
			return err
		}
		m.Online, m.LastSeen, m.Slots = true, e.clock(), hb.FreeSlots
		return tx.SaveMachine(ctx, m)
	})
}

// SessionStateChanged records what the daemon says about a session
// process: running (with its native ref) or gone.
func (e *Engine) SessionStateChanged(ctx context.Context, machineID ID, r protocol.SessionStateReport) error {
	err := e.store.Atomic(ctx, func(tx Tx) error {
		ses, err := sessionOn(ctx, tx, machineID, ID(r.SessionID))
		if err != nil {
			return err
		}
		ses.State, ses.UpdatedAt = r.State, e.clock()
		ses.Live = r.State != ir.SessionExited && r.State != SessionStopped
		if !ses.Live {
			ses.State = SessionStopped
		}
		if r.Ref != nil {
			ses.Ref = r.Ref
		}
		return tx.SaveSession(ctx, ses)
	})
	if err == nil {
		e.wake()
	}
	return err
}

// SessionEvent stores a reliable IR event, moves the session and its job,
// and pushes the event to the clients. Deltas are only pushed.
func (e *Engine) SessionEvent(ctx context.Context, machineID ID, se protocol.SessionEvent) error {
	ev := se.Event
	var (
		owner   ID
		note    protocol.SessionEventNote
		job     *Job
		agentNm string
		fresh   = true
		post    afterCommit
	)
	err := e.store.Atomic(ctx, func(tx Tx) error {
		post, job = nil, nil
		ses, err := sessionOn(ctx, tx, machineID, ID(ev.SessionID))
		if err != nil {
			return err
		}
		ag, err := tx.Agent(ctx, ses.AgentID)
		if err != nil {
			return err
		}
		m, err := tx.Machine(ctx, machineID)
		if err != nil {
			return err
		}
		owner, agentNm = m.OwnerID, ag.Name
		note = protocol.SessionEventNote{SessionID: string(ses.ID), Agent: ag.Name, JobID: string(ses.JobID), Super: ag.Role == RoleSuper, Event: ev}
		if !ev.Type.Reliable() {
			return nil
		}
		if fresh, err = tx.AddSessionEvent(ctx, ses.ID, ev); err != nil || !fresh {
			return err
		}
		now := e.clock()
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
			ses.State, ses.Live = SessionStopped, false
		}
		ses.UpdatedAt = now
		if err := tx.SaveSession(ctx, ses); err != nil {
			return err
		}
		if ses.JobID == "" {
			return e.superEvent(ctx, tx, ses, ev, &post)
		}
		j, err := tx.Job(ctx, ses.JobID)
		if err != nil {
			return err
		}
		if changed := applyJobEvent(&j, ev); !changed {
			return nil
		}
		j.UpdatedAt = now
		if err := tx.SaveJob(ctx, j); err != nil {
			return err
		}
		job = &j
		if j.State.Terminal() {
			return e.jobFinished(ctx, tx, j, ag, &post)
		}
		return nil
	})
	if err != nil {
		return err
	}
	post.run()
	if fresh {
		e.publish(ctx, owner, PushSession, note)
	}
	if job != nil {
		e.publish(ctx, owner, PushJob, WireJob(*job, agentNm))
		if job.State.Terminal() {
			e.wake()
		}
	}
	return nil
}

// applyJobEvent moves a job with one event of its session. It reports
// whether the job changed. The result is the last agent message of the
// last turn; a cancelled job stays cancelled.
func applyJobEvent(j *Job, ev ir.Event) bool {
	if j.State == JobCancelled {
		return false
	}
	switch ev.Type {
	case ir.TurnStarted:
		j.State, j.Result, j.Error = JobRunning, "", ""
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
			j.State = JobDone
		case ir.TurnInterrupted:
			j.State, j.Error = JobFailed, "interrupted"
		default:
			j.State, j.Error = JobFailed, "turn failed"
			if ev.Turn.Error != nil && ev.Turn.Error.Message != "" {
				j.Error = ev.Turn.Error.Message
			}
		}
		return true
	case ir.SessionExitedEvent:
		if !j.State.Terminal() {
			j.State, j.Error = JobFailed, "agent process exited"
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
func sessionOn(ctx context.Context, tx Tx, machineID, sessionID ID) (Session, error) {
	ses, err := tx.Session(ctx, sessionID)
	if err != nil {
		return Session{}, fmt.Errorf("session %s: %w", sessionID, err)
	}
	ag, err := tx.Agent(ctx, ses.AgentID)
	if err != nil {
		return Session{}, err
	}
	if ag.MachineID != machineID {
		return Session{}, fmt.Errorf("session %s: %w", sessionID, errs.ErrForbidden)
	}
	return ses, nil
}
