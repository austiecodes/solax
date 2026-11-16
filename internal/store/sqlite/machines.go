package sqlite

import (
	"context"
	"encoding/json"

	"github.com/austiecodes/solax/internal/core"
)

const machineCols = `id, owner_id, name, state, token_hash, pair_code, pair_until, last_seen, online, slots, drivers`

func (t *txn) Machine(ctx context.Context, id core.ID) (core.Machine, error) {
	return scanMachine(t.q.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE id = ?`, id))
}

func (t *txn) MachineByPairCode(ctx context.Context, code string) (core.Machine, error) {
	return scanMachine(t.q.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE pair_code = ? AND pair_code <> ''`, code))
}

func (t *txn) MachineByTokenHash(ctx context.Context, hash string) (core.Machine, error) {
	return scanMachine(t.q.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE token_hash = ?`, hash))
}

func (t *txn) SaveMachine(ctx context.Context, m core.Machine) error {
	drivers, err := jsonText(nonNil(m.Drivers))
	if err != nil {
		return err
	}
	_, err = t.exec(ctx, `INSERT INTO machines(`+machineCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET owner_id = excluded.owner_id, name = excluded.name, state = excluded.state,
			token_hash = excluded.token_hash, pair_code = excluded.pair_code, pair_until = excluded.pair_until,
			last_seen = excluded.last_seen, online = excluded.online, slots = excluded.slots, drivers = excluded.drivers`,
		m.ID, m.OwnerID, m.Name, m.State, m.TokenHash, m.PairCode, ms(m.PairUntil), ms(m.LastSeen), b2i(m.Online), m.Slots, drivers)
	return err
}

func (t *txn) Machines(ctx context.Context) ([]core.Machine, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+machineCols+` FROM machines ORDER BY name, id`)
	return collect(rows, err, scanMachine)
}

func scanMachine(s scanner) (core.Machine, error) {
	var m core.Machine
	var pairUntil, lastSeen int64
	var online int
	var drivers string
	if err := s.Scan(&m.ID, &m.OwnerID, &m.Name, &m.State, &m.TokenHash, &m.PairCode, &pairUntil, &lastSeen, &online, &m.Slots, &drivers); err != nil {
		return m, mapErr(err)
	}
	m.PairUntil, m.LastSeen, m.Online = tm(pairUntil), tm(lastSeen), online != 0
	return m, json.Unmarshal([]byte(drivers), &m.Drivers)
}
