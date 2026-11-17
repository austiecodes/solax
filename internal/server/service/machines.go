package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/server/model"

	"github.com/austiecodes/solax/internal/errs"
)

// RegisterMachine adds an approved machine with the hash of the token that
// the machine generated. A client calls it from the machine itself. A retry
// with the same hash returns the same machine.
func RegisterMachine(ctx context.Context, a model.Actor, name, tokenHash string) (model.Machine, error) {
	if err := requireHuman(a); err != nil {
		return model.Machine{}, err
	}
	if err := validMachine(name, tokenHash); err != nil {
		return model.Machine{}, err
	}
	var m model.Machine
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		existing, err := model.GetMachineByTokenHash(ctx, tx, tokenHash)
		switch {
		case err == nil:
			if existing.OwnerID != a.OwnerID || existing.State == model.MachineRevoked {
				return fmt.Errorf("%w: token already used", errs.ErrConflict)
			}
			// A retry, or a pending machine that its owner registers directly.
			existing.State, existing.PairCode, existing.PairUntil = model.MachineApproved, "", zeroTime
			existing.Name = name
			m = existing
		case errors.Is(err, errs.ErrNotFound):
			m = model.Machine{ID: model.NewIDAt(model.PrefixMachine, clock()), OwnerID: a.OwnerID, Name: name,
				State: model.MachineApproved, TokenHash: tokenHash}
		default:
			return err
		}
		return model.SaveMachine(ctx, tx, m)
	})
	return m, err
}

// EnrollMachine creates a pending machine with a pairing code that is
// valid for PairTTL. The same token hash enrolls once; a retry returns the
// same machine and code.
func EnrollMachine(ctx context.Context, name, tokenHash string) (model.Machine, error) {
	if err := validMachine(name, tokenHash); err != nil {
		return model.Machine{}, err
	}
	var m model.Machine
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		owners, err := model.ListOwners(ctx, tx)
		if err != nil {
			return err
		}
		if len(owners) == 0 {
			return fmt.Errorf("%w: server not initialised", errs.ErrUnavailable)
		}
		existing, err := model.GetMachineByTokenHash(ctx, tx, tokenHash)
		switch {
		case err == nil:
			if existing.State != model.MachinePending {
				m = existing // approved: nothing to do; revoked: stays revoked
				return nil
			}
			if clock().Before(existing.PairUntil) {
				m = existing
				return nil
			}
			existing.PairCode, existing.PairUntil = newPairCode(), clock().Add(model.PairTTL)
			m = existing
		case errors.Is(err, errs.ErrNotFound):
			m = model.Machine{ID: model.NewIDAt(model.PrefixMachine, clock()), OwnerID: owners[0].ID, Name: name,
				State: model.MachinePending, TokenHash: tokenHash, PairCode: newPairCode(), PairUntil: clock().Add(model.PairTTL)}
		default:
			return err
		}
		return model.SaveMachine(ctx, tx, m)
	})
	return m, err
}

// ApproveMachine approves a pending machine by its pairing code.
func ApproveMachine(ctx context.Context, a model.Actor, code string) (model.Machine, error) {
	if err := requireHuman(a); err != nil {
		return model.Machine{}, err
	}
	code = normPairCode(code)
	if code == "" {
		return model.Machine{}, fmt.Errorf("%w: empty pairing code", errs.ErrInvalid)
	}
	var m model.Machine
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		var err error
		m, err = model.GetMachineByPairCode(ctx, tx, code)
		if err != nil {
			return err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return err
		}
		if m.State != model.MachinePending || !clock().Before(m.PairUntil) {
			return fmt.Errorf("%w: pairing code expired", errs.ErrNotFound)
		}
		m.State, m.PairCode, m.PairUntil = model.MachineApproved, "", zeroTime
		return model.SaveMachine(ctx, tx, m)
	})
	return m, err
}

// RevokeMachine makes the token of a machine invalid and closes its
// connection. Its sessions end with the connection.
func RevokeMachine(ctx context.Context, a model.Actor, machineID model.ID) error {
	if err := requireHuman(a); err != nil {
		return err
	}
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		m, err := model.GetMachine(ctx, tx, machineID)
		if err != nil {
			return err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return err
		}
		m.State, m.Online, m.PairCode, m.PairUntil = model.MachineRevoked, false, "", zeroTime
		return model.SaveMachine(ctx, tx, m)
	})
	if err != nil {
		return err
	}
	Disconnect(machineID)
	return nil
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
