package service

import (
	"context"
	"database/sql"
	"strings"

	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Delivery planning. The server delivery loop asks for due work, sends it
// through the hub, and reports the acks back.

// Delivery is the due work of one session.
type Delivery struct {
	MachineID model.ID
	SessionID model.ID
	Ensure    *protocol.SessionEnsure // nil when the session runs
	Input     ir.Input
	InputIDs  []model.ID
}

// PendingDeliveries returns one delivery for each session that has unsent
// inputs and whose machine is online.
func PendingDeliveries(ctx context.Context) ([]Delivery, error) {
	var out []Delivery
	err := model.Snapshot(ctx, func(tx *sql.Tx) error {
		ids, err := model.ListPendingSessions(ctx, tx)
		if err != nil {
			return err
		}
		for _, sid := range ids {
			d, ok, err := planDelivery(ctx, tx, sid)
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

func planDelivery(ctx context.Context, db model.Querier, sid model.ID) (Delivery, bool, error) {
	ses, err := model.GetSession(ctx, db, sid)
	if err != nil {
		return Delivery{}, false, err
	}
	ag, err := model.GetAgent(ctx, db, ses.AgentID)
	if err != nil {
		return Delivery{}, false, err
	}
	m, err := model.GetMachine(ctx, db, ag.MachineID)
	if err != nil {
		return Delivery{}, false, err
	}
	if ag.Deleted || !m.Online || m.State != model.MachineApproved || !Online(m.ID) {
		return Delivery{}, false, nil
	}
	pending, err := model.ListPendingInputs(ctx, db, sid)
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
		ens, err := ensureFor(ctx, ses, ag, m)
		if err != nil {
			return Delivery{}, false, err
		}
		d.Ensure = &ens
	}
	return d, true, nil
}

func ensureFor(ctx context.Context, ses model.Session, ag model.Agent, m model.Machine) (protocol.SessionEnsure, error) {
	role := protocol.RoleWorker
	if ag.Role == model.RoleSuper {
		role = protocol.RoleSuper
	}
	ens := protocol.SessionEnsure{
		SessionID: string(ses.ID), Agent: ag.Name, Role: role, Task: string(ses.TaskID),
		Runtime: ag.Runtime, Dir: ag.Dir, Model: ag.Model, Options: ag.Options, Resume: ses.Ref,
		Instructions: ag.Instructions,
		LastSeq:      uint64(ses.LastSeq),
	}
	tok, err := IssueToken(ctx, model.TokenRequest{OwnerID: m.OwnerID, AgentID: ag.ID, SessionID: ses.ID, Task: ses.TaskID, Super: ag.Role == model.RoleSuper})
	if err != nil {
		return ens, err
	}
	ens.AgentToken = tok
	return ens, nil
}

// Delivered records that the daemon accepted a batch.
func Delivered(ctx context.Context, sessionID model.ID, inputIDs []model.ID) error {
	return model.Atomic(ctx, func(tx *sql.Tx) error {
		if err := model.MarkInputsSent(ctx, tx, inputIDs); err != nil {
			return err
		}
		ses, err := model.GetSession(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		ses.Live, ses.UpdatedAt = true, clock()
		return model.SaveSession(ctx, tx, ses)
	})
}

// SessionLost records that the daemon does not run a session that the
// server thought was live. The next delivery pass starts it again.
func SessionLost(ctx context.Context, sessionID model.ID) error {
	return model.Atomic(ctx, func(tx *sql.Tx) error {
		ses, err := model.GetSession(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		ses.Live, ses.State, ses.UpdatedAt = false, model.SessionStopped, clock()
		return model.SaveSession(ctx, tx, ses)
	})
}

// DeliveryFailed records that a session could not start. Its task fails;
// the inputs stay pending for the Super Agent session, which retries.
func DeliveryFailed(ctx context.Context, sessionID model.ID, reason string) error {
	var task *model.Task
	var name string
	var post afterCommit
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		post, task = nil, nil
		ses, err := model.GetSession(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		if ses.TaskID == "" {
			return nil
		}
		j, err := model.GetTask(ctx, tx, ses.TaskID)
		if err != nil {
			return err
		}
		pending, err := model.ListPendingInputs(ctx, tx, sessionID)
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
		ag, err := model.GetAgent(ctx, tx, j.AgentID)
		if err != nil {
			return err
		}
		j.State, j.Error, j.UpdatedAt = model.TaskFailed, reason, clock()
		task, name = &j, ag.Name
		if err := model.SaveTask(ctx, tx, j); err != nil {
			return err
		}
		return taskFinished(ctx, tx, j, ag, &post)
	})
	post.run()
	if task != nil {
		Publish(ctx, task.OwnerID, PushTask, model.WireTask(*task, name))
		Wake()
	}
	return err
}
