package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Client stream events (pushes). The payloads are protocol DTOs.
const (
	PushAgents   = "agents.changed"   // no payload: reload the agent list
	PushMachines = "machines.changed" // no payload: reload the machine list
	PushJob      = "job"              // protocol.Job
	PushSession  = "session.event"    // protocol.SessionEventNote
	PushMessage  = "super.message"    // protocol.Message of the Super Agent conversation
)

// publish pushes a client event. The stream is best-effort: a client that
// missed an event reloads from the REST API.
func (e *Engine) publish(ctx context.Context, owner ID, event string, payload any) {
	if e.notify != nil {
		_ = e.notify.Publish(ctx, owner, event, payload)
	}
}

// OnWork sets the function that wakes the delivery loop. The server sets
// it; the engine calls it after a commit that added inputs or brought a
// machine online.
func (e *Engine) OnWork(fn func()) { e.kick = fn }

func (e *Engine) wake() {
	if e.kick != nil {
		e.kick()
	}
}

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
		if err := tx.Sessions().Save(ctx, Session{ID: j.SessionID, AgentID: ag.ID, JobID: j.ID, State: SessionStopped, UpdatedAt: now}); err != nil {
			return err
		}
		if err := tx.Jobs().Save(ctx, j); err != nil {
			return err
		}
		return tx.Inputs().Add(ctx, Input{ID: NewIDAt(PrefixInput, now), SessionID: j.SessionID, Text: prompt, CreatedAt: now})
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
		if err := tx.Jobs().Save(ctx, j); err != nil {
			return err
		}
		return tx.Inputs().Add(ctx, Input{ID: NewIDAt(PrefixInput, now), SessionID: j.SessionID, Text: text, CreatedAt: now})
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
		pending, err := tx.Inputs().Pending(ctx, j.SessionID)
		if err != nil {
			return err
		}
		ids := make([]ID, len(pending))
		for i, in := range pending {
			ids[i] = in.ID
		}
		if err := tx.Inputs().MarkSent(ctx, ids); err != nil {
			return err
		}
		j.State, j.UpdatedAt = JobCancelled, e.clock()
		if err := tx.Jobs().Save(ctx, j); err != nil {
			return err
		}
		ses, err := tx.Sessions().Get(ctx, j.SessionID)
		if err != nil {
			return err
		}
		ag, err := tx.Agents().Get(ctx, j.AgentID)
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
		out, err = tx.Jobs().List(ctx, a.OwnerID, limit)
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
		ses, err := tx.Sessions().Get(ctx, sessionID)
		if err != nil {
			return err
		}
		ag, err := tx.Agents().Get(ctx, ses.AgentID)
		if err != nil {
			return err
		}
		m, err := tx.Machines().Get(ctx, ag.MachineID)
		if err != nil {
			return err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return err
		}
		out, err = tx.SessionEvents().Since(ctx, sessionID, after, limit)
		return err
	})
	return out, err
}

func ownedJob(ctx context.Context, tx Tx, a Actor, id ID) (Job, error) {
	j, err := tx.Jobs().Get(ctx, id)
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
		ag, err := tx.Agents().Get(ctx, j.AgentID)
		name = ag.Name
		return err
	})
	e.publish(ctx, j.OwnerID, PushJob, JobDTO(j, name))
}

// JobDTO converts a job to its wire form.
func JobDTO(j Job, agentName string) protocol.Job {
	return protocol.Job{
		ID: string(j.ID), Agent: agentName, AgentID: string(j.AgentID), SessionID: string(j.SessionID),
		RequestedBy: string(j.RequestedBy), Title: j.Title, Prompt: j.Prompt, State: string(j.State),
		Result: j.Result, Error: j.Error, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// Daemon facts about sessions.

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
		return tx.Sessions().Save(ctx, ses)
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
		ag, err := tx.Agents().Get(ctx, ses.AgentID)
		if err != nil {
			return err
		}
		m, err := tx.Machines().Get(ctx, machineID)
		if err != nil {
			return err
		}
		owner, agentNm = m.OwnerID, ag.Name
		note = protocol.SessionEventNote{SessionID: string(ses.ID), Agent: ag.Name, JobID: string(ses.JobID), Super: ag.Role == RoleSuper, Event: ev}
		if !ev.Type.Reliable() {
			return nil
		}
		if fresh, err = tx.SessionEvents().Add(ctx, ses.ID, ev); err != nil || !fresh {
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
		if err := tx.Sessions().Save(ctx, ses); err != nil {
			return err
		}
		if ses.JobID == "" {
			return e.superEvent(ctx, tx, ses, ev, &post)
		}
		j, err := tx.Jobs().Get(ctx, ses.JobID)
		if err != nil {
			return err
		}
		if changed := applyJobEvent(&j, ev); !changed {
			return nil
		}
		j.UpdatedAt = now
		if err := tx.Jobs().Save(ctx, j); err != nil {
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
		e.publish(ctx, owner, PushJob, JobDTO(*job, agentNm))
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
	ses, err := tx.Sessions().Get(ctx, sessionID)
	if err != nil {
		return Session{}, fmt.Errorf("session %s: %w", sessionID, err)
	}
	ag, err := tx.Agents().Get(ctx, ses.AgentID)
	if err != nil {
		return Session{}, err
	}
	if ag.MachineID != machineID {
		return Session{}, fmt.Errorf("session %s: %w", sessionID, errs.ErrForbidden)
	}
	return ses, nil
}

// Delivery planning. The server delivery loop asks for due work, sends it
// through the hub, and reports the acks back.

// Delivery is the due work of one session.
type Delivery struct {
	MachineID ID
	SessionID ID
	Ensure    *protocol.SessionEnsure // nil when the session runs
	Input     ir.Input
	InputIDs  []ID
}

// PendingDeliveries returns one delivery for each session that has unsent
// inputs and whose machine is online.
func (e *Engine) PendingDeliveries(ctx context.Context) ([]Delivery, error) {
	var out []Delivery
	err := e.store.Read(ctx, func(tx Tx) error {
		ids, err := tx.Inputs().PendingSessions(ctx)
		if err != nil {
			return err
		}
		for _, sid := range ids {
			d, ok, err := e.planDelivery(ctx, tx, sid)
			if err != nil {
				return err
			}
			if ok {
				out = append(out, d)
			}
		}
		return nil
	})
	return out, err
}

func (e *Engine) planDelivery(ctx context.Context, tx Tx, sid ID) (Delivery, bool, error) {
	ses, err := tx.Sessions().Get(ctx, sid)
	if err != nil {
		return Delivery{}, false, err
	}
	ag, err := tx.Agents().Get(ctx, ses.AgentID)
	if err != nil {
		return Delivery{}, false, err
	}
	m, err := tx.Machines().Get(ctx, ag.MachineID)
	if err != nil {
		return Delivery{}, false, err
	}
	if ag.Deleted || !m.Online || m.State != MachineApproved || (e.dispatch != nil && !e.dispatch.Online(m.ID)) {
		return Delivery{}, false, nil
	}
	pending, err := tx.Inputs().Pending(ctx, sid)
	if err != nil || len(pending) == 0 {
		return Delivery{}, false, err
	}
	d := Delivery{MachineID: m.ID, SessionID: sid}
	texts := make([]string, len(pending))
	for i, in := range pending {
		texts[i] = in.Text
		d.InputIDs = append(d.InputIDs, in.ID)
	}
	// The ref is the first input: a batch that is resent after a lost ack
	// has the same first input, so the daemon drops it.
	d.Input = ir.Input{Mode: ir.ModeFollowUp, Text: strings.Join(texts, "\n\n---\n\n"), Ref: string(pending[0].ID)}
	if !ses.Live {
		ens, err := e.ensureFor(ctx, ses, ag, m)
		if err != nil {
			return Delivery{}, false, err
		}
		d.Ensure = &ens
	}
	return d, true, nil
}

func (e *Engine) ensureFor(ctx context.Context, ses Session, ag Agent, m Machine) (protocol.SessionEnsure, error) {
	role := protocol.RoleMember
	if ag.Role == RoleSuper {
		role = protocol.RoleSuper
	}
	ens := protocol.SessionEnsure{
		SessionID: string(ses.ID), Agent: ag.Name, Role: role, Job: string(ses.JobID),
		Driver: ag.Driver, Dir: ag.Dir, Model: ag.Model, Options: ag.Options, Resume: ses.Ref,
	}
	if e.tokens != nil {
		tok, err := e.tokens.IssueToken(ctx, TokenRequest{OwnerID: m.OwnerID, AgentID: ag.ID, SessionID: ses.ID, Task: ses.JobID, Super: ag.Role == RoleSuper})
		if err != nil {
			return ens, err
		}
		ens.AgentToken = tok
	}
	return ens, nil
}

// Delivered records that the daemon accepted a batch.
func (e *Engine) Delivered(ctx context.Context, sessionID ID, inputIDs []ID) error {
	return e.store.Atomic(ctx, func(tx Tx) error {
		if err := tx.Inputs().MarkSent(ctx, inputIDs); err != nil {
			return err
		}
		ses, err := tx.Sessions().Get(ctx, sessionID)
		if err != nil {
			return err
		}
		ses.Live, ses.UpdatedAt = true, e.clock()
		return tx.Sessions().Save(ctx, ses)
	})
}

// SessionLost records that the daemon does not run a session that the
// server thought was live. The next delivery pass starts it again.
func (e *Engine) SessionLost(ctx context.Context, sessionID ID) error {
	return e.store.Atomic(ctx, func(tx Tx) error {
		ses, err := tx.Sessions().Get(ctx, sessionID)
		if err != nil {
			return err
		}
		ses.Live, ses.State, ses.UpdatedAt = false, SessionStopped, e.clock()
		return tx.Sessions().Save(ctx, ses)
	})
}

// DeliveryFailed records that a session could not start. Its job fails;
// the inputs stay pending for the Super Agent session, which retries.
func (e *Engine) DeliveryFailed(ctx context.Context, sessionID ID, reason string) error {
	var job *Job
	var name string
	var post afterCommit
	err := e.store.Atomic(ctx, func(tx Tx) error {
		post, job = nil, nil
		ses, err := tx.Sessions().Get(ctx, sessionID)
		if err != nil {
			return err
		}
		if ses.JobID == "" {
			return nil
		}
		j, err := tx.Jobs().Get(ctx, ses.JobID)
		if err != nil {
			return err
		}
		pending, err := tx.Inputs().Pending(ctx, sessionID)
		if err != nil {
			return err
		}
		ids := make([]ID, len(pending))
		for i, in := range pending {
			ids[i] = in.ID
		}
		if err := tx.Inputs().MarkSent(ctx, ids); err != nil {
			return err
		}
		ag, err := tx.Agents().Get(ctx, j.AgentID)
		if err != nil {
			return err
		}
		j.State, j.Error, j.UpdatedAt = JobFailed, reason, e.clock()
		job, name = &j, ag.Name
		if err := tx.Jobs().Save(ctx, j); err != nil {
			return err
		}
		return e.jobFinished(ctx, tx, j, ag, &post)
	})
	post.run()
	if job != nil {
		e.publish(ctx, job.OwnerID, PushJob, JobDTO(*job, name))
		e.wake()
	}
	return err
}
