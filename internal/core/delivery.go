package core

import (
	"context"
	"strings"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

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
		ids, err := tx.PendingSessions(ctx)
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
	ses, err := tx.Session(ctx, sid)
	if err != nil {
		return Delivery{}, false, err
	}
	ag, err := tx.Agent(ctx, ses.AgentID)
	if err != nil {
		return Delivery{}, false, err
	}
	m, err := tx.Machine(ctx, ag.MachineID)
	if err != nil {
		return Delivery{}, false, err
	}
	if ag.Deleted || !m.Online || m.State != MachineApproved || (e.dispatch != nil && !e.dispatch.Online(m.ID)) {
		return Delivery{}, false, nil
	}
	pending, err := tx.PendingInputs(ctx, sid)
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
	role := protocol.RoleWorker
	if ag.Role == RoleSuper {
		role = protocol.RoleSuper
	}
	ens := protocol.SessionEnsure{
		SessionID: string(ses.ID), Agent: ag.Name, Role: role, Job: string(ses.JobID),
		Driver: ag.Driver, Dir: ag.Dir, Model: ag.Model, Options: ag.Options, Resume: ses.Ref,
		Instructions: ag.Instructions,
		LastSeq:      uint64(ses.LastSeq),
	}
	if e.tokens != nil {
		tok, err := e.tokens.IssueToken(ctx, TokenRequest{OwnerID: m.OwnerID, AgentID: ag.ID, SessionID: ses.ID, Job: ses.JobID, Super: ag.Role == RoleSuper})
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
		if err := tx.MarkInputsSent(ctx, inputIDs); err != nil {
			return err
		}
		ses, err := tx.Session(ctx, sessionID)
		if err != nil {
			return err
		}
		ses.Live, ses.UpdatedAt = true, e.clock()
		return tx.SaveSession(ctx, ses)
	})
}

// SessionLost records that the daemon does not run a session that the
// server thought was live. The next delivery pass starts it again.
func (e *Engine) SessionLost(ctx context.Context, sessionID ID) error {
	return e.store.Atomic(ctx, func(tx Tx) error {
		ses, err := tx.Session(ctx, sessionID)
		if err != nil {
			return err
		}
		ses.Live, ses.State, ses.UpdatedAt = false, SessionStopped, e.clock()
		return tx.SaveSession(ctx, ses)
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
		ses, err := tx.Session(ctx, sessionID)
		if err != nil {
			return err
		}
		if ses.JobID == "" {
			return nil
		}
		j, err := tx.Job(ctx, ses.JobID)
		if err != nil {
			return err
		}
		pending, err := tx.PendingInputs(ctx, sessionID)
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
		ag, err := tx.Agent(ctx, j.AgentID)
		if err != nil {
			return err
		}
		j.State, j.Error, j.UpdatedAt = JobFailed, reason, e.clock()
		job, name = &j, ag.Name
		if err := tx.SaveJob(ctx, j); err != nil {
			return err
		}
		return e.jobFinished(ctx, tx, j, ag, &post)
	})
	post.run()
	if job != nil {
		e.publish(ctx, job.OwnerID, PushJob, WireJob(*job, name))
		e.wake()
	}
	return err
}
