package model

import (
	"context"
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Machine is a host that runs "solax daemon".
type Machine struct {
	ID        ID
	OwnerID   ID
	Name      string
	State     MachineState
	TokenHash string    // SHA-256 of the token that the machine generated
	PairCode  string    // pending only: the code that a client approves
	PairUntil time.Time // pending only: the code expires
	LastSeen  time.Time
	Online    bool
	Slots     int // free session slots from the last heartbeat
	// Runtimes are the coding agent CLIs that the daemon detected.
	Runtimes []protocol.RuntimeProbe
}

// HasRuntime reports whether the daemon detected a working runtime of kind k.
func (m Machine) HasRuntime(k ir.Kind) bool {
	for _, d := range m.Runtimes {
		if d.Kind == k && d.OK {
			return true
		}
	}
	return false
}

// Reports tells whether the daemon reported a runtime of kind k, working
// or not.
func (m Machine) Reports(k ir.Kind) bool {
	for _, d := range m.Runtimes {
		if d.Kind == k {
			return true
		}
	}
	return false
}

// MachineState is the approval state of a machine.
type MachineState string

const (
	MachinePending  MachineState = "pending"  // enrolled, waits for a client to approve
	MachineApproved MachineState = "approved" // its token is valid
	MachineRevoked  MachineState = "revoked"
)

// PairTTL is how long a pairing code is valid.
const PairTTL = 24 * time.Hour

const machineCols = `id, owner_id, name, state, token_hash, pair_code, pair_until, last_seen, online, slots, runtimes`

// GetMachine returns the machine with this ID.
func GetMachine(ctx context.Context, db Querier, id ID) (Machine, error) {
	return scanMachine(db.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE id = ?`, id))
}

// GetMachineByPairCode returns the pending machine with this pairing code.
func GetMachineByPairCode(ctx context.Context, db Querier, code string) (Machine, error) {
	return scanMachine(db.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE pair_code = ? AND pair_code <> ''`, code))
}

// GetMachineByTokenHash returns the machine whose token has this hash.
func GetMachineByTokenHash(ctx context.Context, db Querier, hash string) (Machine, error) {
	return scanMachine(db.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE token_hash = ?`, hash))
}

// SaveMachine inserts or updates a machine by ID.
func SaveMachine(ctx context.Context, db Executor, m Machine) error {
	runtimes, err := jsonText(nonNil(m.Runtimes))
	if err != nil {
		return err
	}
	_, err = exec(ctx, db, `INSERT INTO machines(`+machineCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET owner_id = excluded.owner_id, name = excluded.name, state = excluded.state,
			token_hash = excluded.token_hash, pair_code = excluded.pair_code, pair_until = excluded.pair_until,
			last_seen = excluded.last_seen, online = excluded.online, slots = excluded.slots, runtimes = excluded.runtimes`,
		m.ID, m.OwnerID, m.Name, m.State, m.TokenHash, m.PairCode, ms(m.PairUntil), ms(m.LastSeen), b2i(m.Online), m.Slots, runtimes)
	return err
}

// ListMachines returns the machines of an owner, by name.
func ListMachines(ctx context.Context, db Querier, ownerID ID) ([]Machine, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+machineCols+` FROM machines WHERE owner_id = ? ORDER BY name, id`, ownerID)
	return collect(rows, err, scanMachine)
}

func scanMachine(s scanner) (Machine, error) {
	var m Machine
	var pairUntil, lastSeen int64
	var online int
	var runtimes string
	if err := s.Scan(&m.ID, &m.OwnerID, &m.Name, &m.State, &m.TokenHash, &m.PairCode, &pairUntil, &lastSeen, &online, &m.Slots, &runtimes); err != nil {
		return m, mapErr(err)
	}
	m.PairUntil, m.LastSeen, m.Online = tm(pairUntil), tm(lastSeen), online != 0
	return m, json.Unmarshal([]byte(runtimes), &m.Runtimes)
}
