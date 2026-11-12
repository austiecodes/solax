package core

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// RegisterMachine adds an approved machine with the hash of the token that
// the machine generated. A client calls it from the machine itself. A retry
// with the same hash returns the same machine.
func (e *Engine) RegisterMachine(ctx context.Context, a Actor, name, tokenHash string) (Machine, error) {
	if err := requireHuman(a); err != nil {
		return Machine{}, err
	}
	if err := validMachine(name, tokenHash); err != nil {
		return Machine{}, err
	}
	var m Machine
	err := e.store.Atomic(ctx, func(tx Tx) error {
		existing, err := tx.Machines().ByTokenHash(ctx, tokenHash)
		switch {
		case err == nil:
			if existing.OwnerID != a.OwnerID || existing.State == MachineRevoked {
				return fmt.Errorf("%w: token already used", errs.ErrConflict)
			}
			// A retry, or a pending machine that its owner registers directly.
			existing.State, existing.PairCode, existing.PairUntil = MachineApproved, "", zeroTime
			existing.Name = name
			m = existing
		case errors.Is(err, errs.ErrNotFound):
			m = Machine{ID: NewIDAt(PrefixMachine, e.clock()), OwnerID: a.OwnerID, Name: name,
				State: MachineApproved, TokenHash: tokenHash}
		default:
			return err
		}
		return tx.Machines().Save(ctx, m)
	})
	return m, err
}

// EnrollMachine creates a pending machine with a pairing code that is
// valid for PairTTL. The same token hash enrolls once; a retry returns the
// same machine and code.
func (e *Engine) EnrollMachine(ctx context.Context, name, tokenHash string) (Machine, error) {
	if err := validMachine(name, tokenHash); err != nil {
		return Machine{}, err
	}
	var m Machine
	err := e.store.Atomic(ctx, func(tx Tx) error {
		owners, err := tx.Owners().List(ctx)
		if err != nil {
			return err
		}
		if len(owners) == 0 {
			return fmt.Errorf("%w: server not initialised", errs.ErrUnavailable)
		}
		existing, err := tx.Machines().ByTokenHash(ctx, tokenHash)
		switch {
		case err == nil:
			if existing.State != MachinePending {
				m = existing // approved: nothing to do; revoked: stays revoked
				return nil
			}
			if e.clock().Before(existing.PairUntil) {
				m = existing
				return nil
			}
			existing.PairCode, existing.PairUntil = newPairCode(), e.clock().Add(PairTTL)
			m = existing
		case errors.Is(err, errs.ErrNotFound):
			m = Machine{ID: NewIDAt(PrefixMachine, e.clock()), OwnerID: owners[0].ID, Name: name,
				State: MachinePending, TokenHash: tokenHash, PairCode: newPairCode(), PairUntil: e.clock().Add(PairTTL)}
		default:
			return err
		}
		return tx.Machines().Save(ctx, m)
	})
	return m, err
}

// ApproveMachine approves a pending machine by its pairing code.
func (e *Engine) ApproveMachine(ctx context.Context, a Actor, code string) (Machine, error) {
	if err := requireHuman(a); err != nil {
		return Machine{}, err
	}
	code = normPairCode(code)
	if code == "" {
		return Machine{}, fmt.Errorf("%w: empty pairing code", errs.ErrInvalid)
	}
	var m Machine
	err := e.store.Atomic(ctx, func(tx Tx) error {
		var err error
		m, err = tx.Machines().ByPairCode(ctx, code)
		if err != nil {
			return err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return err
		}
		if m.State != MachinePending || !e.clock().Before(m.PairUntil) {
			return fmt.Errorf("%w: pairing code expired", errs.ErrNotFound)
		}
		m.State, m.PairCode, m.PairUntil = MachineApproved, "", zeroTime
		return tx.Machines().Save(ctx, m)
	})
	return m, err
}

// RevokeMachine makes the token of a machine invalid and closes its
// connection. Its sessions end with the connection.
func (e *Engine) RevokeMachine(ctx context.Context, a Actor, machineID ID) error {
	if err := requireHuman(a); err != nil {
		return err
	}
	err := e.store.Atomic(ctx, func(tx Tx) error {
		m, err := tx.Machines().Get(ctx, machineID)
		if err != nil {
			return err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return err
		}
		m.State, m.Online, m.PairCode, m.PairUntil = MachineRevoked, false, "", zeroTime
		return tx.Machines().Save(ctx, m)
	})
	if err != nil {
		return err
	}
	if e.dispatch != nil {
		e.dispatch.Disconnect(machineID)
	}
	return nil
}

// MachineConnected records that the machine is online with the drivers
// that its daemon detected. Sessions that the daemon still runs stay live;
// all other sessions of the machine are stopped, and the delivery loop
// starts them again when they have input.
func (e *Engine) MachineConnected(ctx context.Context, machineID ID, h protocol.Hello) error {
	var owner ID
	err := e.store.Atomic(ctx, func(tx Tx) error {
		m, err := tx.Machines().Get(ctx, machineID)
		if err != nil {
			return err
		}
		if m.State != MachineApproved {
			return errs.ErrForbidden
		}
		m.Online, m.LastSeen, m.Slots, m.Drivers = true, e.clock(), h.FreeSlots, h.Drivers
		owner = m.OwnerID
		if err := tx.Machines().Save(ctx, m); err != nil {
			return err
		}
		sessions, err := tx.Sessions().OnMachine(ctx, machineID)
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
			if err := tx.Sessions().Save(ctx, ses); err != nil {
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
		m, err := tx.Machines().Get(ctx, machineID)
		if err != nil {
			return err
		}
		owner = m.OwnerID
		m.Online, m.LastSeen = false, e.clock()
		return tx.Machines().Save(ctx, m)
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
		m, err := tx.Machines().Get(ctx, machineID)
		if err != nil {
			return err
		}
		m.Online, m.LastSeen, m.Slots = true, e.clock(), hb.FreeSlots
		return tx.Machines().Save(ctx, m)
	})
}

func validMachine(name, tokenHash string) error {
	if strings.TrimSpace(name) == "" || len(name) > 64 {
		return fmt.Errorf("%w: machine name must be 1-64 characters", errs.ErrInvalid)
	}
	if len(tokenHash) != 64 {
		return fmt.Errorf("%w: token_hash must be a hex SHA-256", errs.ErrInvalid)
	}
	return nil
}

// pairAlphabet has no 0/O, 1/I/L, so a code is easy to read aloud.
const pairAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// newPairCode returns a code such as "K7QM-2XRP" (40 bits).
func newPairCode() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	out := make([]byte, 0, 9)
	for i, v := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, pairAlphabet[int(v)%len(pairAlphabet)])
	}
	return string(out)
}

// normPairCode accepts lower case and a missing dash.
func normPairCode(code string) string {
	c := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	if len(c) != 8 {
		return ""
	}
	return c[:4] + "-" + c[4:]
}
