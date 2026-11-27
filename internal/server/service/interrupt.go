package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Interrupts stop the running turn of a session, as the stop button of a
// chat app does. The session stays: the next input starts a new turn. An
// interrupt does not go through the inbox; the daemon gets it at once.

// InterruptSuper stops the running turn of the Super Agent. Inputs that
// wait in its inbox still go to it. When no session runs, there is
// nothing to stop.
func InterruptSuper(ctx context.Context, a model.Actor) error {
	if err := requireHuman(a); err != nil {
		return err
	}
	var (
		ag  model.Agent
		ses model.Session
	)
	err := model.Snapshot(ctx, func(tx *sql.Tx) error {
		var err error
		if ag, err = superAgent(ctx, tx); err != nil {
			return err
		}
		ses, err = model.GetSessionFor(ctx, tx, ag.ID, "")
		return err
	})
	if errors.Is(err, errs.ErrNotFound) && ag.ID != "" {
		return nil // no session yet
	}
	if err != nil {
		return err
	}
	return interrupt(ctx, ag, ses)
}

// InterruptTask stops the running turn of a task. The turn ends as
// interrupted, so the task fails; more input starts it again. A task that
// does not run is returned as it is.
func InterruptTask(ctx context.Context, a model.Actor, taskID model.ID) (model.Task, error) {
	if err := requireManager(a); err != nil {
		return model.Task{}, err
	}
	var (
		j   model.Task
		ag  model.Agent
		ses model.Session
	)
	err := model.Snapshot(ctx, func(tx *sql.Tx) error {
		var err error
		if j, err = ownedTask(ctx, tx, a, taskID); err != nil || j.State != model.TaskRunning {
			return err
		}
		if ag, err = model.GetAgent(ctx, tx, j.AgentID); err != nil {
			return err
		}
		ses, err = model.GetSession(ctx, tx, j.SessionID)
		return err
	})
	if err != nil || j.State != model.TaskRunning {
		return j, err
	}
	return j, interrupt(ctx, ag, ses)
}

// interrupt sends an interrupt to ses, when it runs on the machine of ag.
func interrupt(ctx context.Context, ag model.Agent, ses model.Session) error {
	if !ses.Live || !Online(ag.MachineID) {
		return nil
	}
	env := protocol.Envelope{Type: protocol.TypeSessionDeliver, Payload: mustJSON(protocol.SessionDeliver{
		SessionID: string(ses.ID), Input: ir.Input{Mode: ir.ModeInterrupt},
	})}
	ack, err := Send(ctx, ag.MachineID, env)
	if err != nil {
		return err
	}
	if !ack.OK {
		return fmt.Errorf("%w: interrupt: %s", errs.ErrUnavailable, ack.Error)
	}
	return nil
}
