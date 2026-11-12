package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/pkg/ir"
)

// scanner is *sql.Row or *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

// collect scans all rows with scan.
func collect[T any](rows *sql.Rows, err error, scan func(scanner) (T, error)) ([]T, error) {
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, mapErr(rows.Err())
}

// Owners.

type ownerRepo struct{ t *txn }

func (r ownerRepo) Get(ctx context.Context, id core.ID) (core.Owner, error) {
	return scanOwner(r.t.q.QueryRowContext(ctx, `SELECT id, created_at FROM owners WHERE id = ?`, id))
}

func (r ownerRepo) Save(ctx context.Context, o core.Owner) error {
	_, err := r.t.exec(ctx, `INSERT INTO owners(id, created_at) VALUES (?, ?)
		ON CONFLICT(id) DO NOTHING`, o.ID, ms(o.CreatedAt))
	return err
}

func (r ownerRepo) List(ctx context.Context) ([]core.Owner, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT id, created_at FROM owners ORDER BY created_at, id`)
	return collect(rows, err, scanOwner)
}

func scanOwner(s scanner) (core.Owner, error) {
	var o core.Owner
	var created int64
	if err := s.Scan(&o.ID, &created); err != nil {
		return o, mapErr(err)
	}
	o.CreatedAt = tm(created)
	return o, nil
}

// Machines.

type machineRepo struct{ t *txn }

const machineCols = `id, owner_id, name, state, token_hash, pair_code, pair_until, last_seen, online, slots, drivers`

func (r machineRepo) Get(ctx context.Context, id core.ID) (core.Machine, error) {
	return scanMachine(r.t.q.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE id = ?`, id))
}

func (r machineRepo) ByPairCode(ctx context.Context, code string) (core.Machine, error) {
	return scanMachine(r.t.q.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE pair_code = ? AND pair_code <> ''`, code))
}

func (r machineRepo) ByTokenHash(ctx context.Context, hash string) (core.Machine, error) {
	return scanMachine(r.t.q.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE token_hash = ?`, hash))
}

func (r machineRepo) Save(ctx context.Context, m core.Machine) error {
	drivers, err := jsonText(nonNil(m.Drivers))
	if err != nil {
		return err
	}
	_, err = r.t.exec(ctx, `INSERT INTO machines(`+machineCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET owner_id = excluded.owner_id, name = excluded.name, state = excluded.state,
			token_hash = excluded.token_hash, pair_code = excluded.pair_code, pair_until = excluded.pair_until,
			last_seen = excluded.last_seen, online = excluded.online, slots = excluded.slots, drivers = excluded.drivers`,
		m.ID, m.OwnerID, m.Name, m.State, m.TokenHash, m.PairCode, ms(m.PairUntil), ms(m.LastSeen), b2i(m.Online), m.Slots, drivers)
	return err
}

func (r machineRepo) List(ctx context.Context) ([]core.Machine, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+machineCols+` FROM machines ORDER BY name, id`)
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

// Agents.

type agentRepo struct{ t *txn }

const agentCols = `id, machine_id, name, description, driver, role, dir, model, options, deleted, created_at`

func (r agentRepo) Get(ctx context.Context, id core.ID) (core.Agent, error) {
	return scanAgent(r.t.q.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
}

func (r agentRepo) ByName(ctx context.Context, machineID core.ID, name string) (core.Agent, error) {
	return scanAgent(r.t.q.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE machine_id = ? AND name = ? AND deleted = 0`, machineID, name))
}

func (r agentRepo) Named(ctx context.Context, name string) (core.Agent, error) {
	return scanAgent(r.t.q.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE name = ? AND deleted = 0`, name))
}

// Save inserts or updates an agent by ID. A second live agent with the
// same name is a conflict.
func (r agentRepo) Save(ctx context.Context, a core.Agent) error {
	opts, err := jsonText(a.Options)
	if err != nil {
		return err
	}
	if a.Options == nil {
		opts = "{}"
	}
	_, err = r.t.exec(ctx, `INSERT INTO agents(`+agentCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET machine_id = excluded.machine_id, name = excluded.name,
			description = excluded.description, driver = excluded.driver, role = excluded.role, dir = excluded.dir,
			model = excluded.model, options = excluded.options, deleted = excluded.deleted`,
		a.ID, a.MachineID, a.Name, a.Description, a.Driver, a.Role, a.Dir, a.Model, opts, b2i(a.Deleted), ms(a.CreatedAt))
	return err
}

func (r agentRepo) List(ctx context.Context) ([]core.Agent, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+agentCols+` FROM agents WHERE deleted = 0 ORDER BY name`)
	return collect(rows, err, scanAgent)
}

func scanAgent(s scanner) (core.Agent, error) {
	var a core.Agent
	var driver, role, opts string
	var deleted int
	var created int64
	if err := s.Scan(&a.ID, &a.MachineID, &a.Name, &a.Description, &driver, &role, &a.Dir, &a.Model, &opts, &deleted, &created); err != nil {
		return a, mapErr(err)
	}
	a.Driver, a.Role, a.Deleted, a.CreatedAt = ir.Kind(driver), core.AgentRole(role), deleted != 0, tm(created)
	if opts != "" && opts != "{}" {
		if err := json.Unmarshal([]byte(opts), &a.Options); err != nil {
			return a, err
		}
	}
	return a, nil
}

// Workspaces and members.

type workspaceRepo struct{ t *txn }

const workspaceCols = `id, owner_id, name, leader_id, orchestrator, next_seq, created_at`

func (r workspaceRepo) Get(ctx context.Context, id core.ID) (core.Workspace, error) {
	return scanWorkspace(r.t.q.QueryRowContext(ctx, `SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, id))
}

func (r workspaceRepo) ByName(ctx context.Context, ownerID core.ID, name string) (core.Workspace, error) {
	return scanWorkspace(r.t.q.QueryRowContext(ctx, `SELECT `+workspaceCols+` FROM workspaces WHERE owner_id = ? AND name = ?`, ownerID, name))
}

// Save inserts or updates a workspace. It never changes next_seq: only
// MessageRepo.Append advances it.
func (r workspaceRepo) Save(ctx context.Context, w core.Workspace) error {
	_, err := r.t.exec(ctx, `INSERT INTO workspaces(`+workspaceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET owner_id = excluded.owner_id, name = excluded.name,
			leader_id = excluded.leader_id, orchestrator = excluded.orchestrator`,
		w.ID, w.OwnerID, w.Name, w.LeaderID, w.Orchestrator, w.NextSeq, ms(w.CreatedAt))
	return err
}

func (r workspaceRepo) List(ctx context.Context) ([]core.Workspace, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT `+workspaceCols+` FROM workspaces ORDER BY name, id`)
	return collect(rows, err, scanWorkspace)
}

func (r workspaceRepo) Members(ctx context.Context, id core.ID) ([]core.Member, error) {
	rows, err := r.t.q.QueryContext(ctx, `SELECT workspace_id, agent_id, role FROM members WHERE workspace_id = ? ORDER BY agent_id`, id)
	return collect(rows, err, func(s scanner) (core.Member, error) {
		var m core.Member
		var role string
		err := s.Scan(&m.WorkspaceID, &m.AgentID, &role)
		m.Role = core.MemberRole(role)
		return m, mapErr(err)
	})
}

func (r workspaceRepo) SaveMember(ctx context.Context, m core.Member) error {
	_, err := r.t.exec(ctx, `INSERT INTO members(workspace_id, agent_id, role) VALUES (?, ?, ?)
		ON CONFLICT(workspace_id, agent_id) DO UPDATE SET role = excluded.role`, m.WorkspaceID, m.AgentID, m.Role)
	return err
}

func (r workspaceRepo) DeleteMember(ctx context.Context, workspaceID, agentID core.ID) error {
	return mustAffect(r.t.exec(ctx, `DELETE FROM members WHERE workspace_id = ? AND agent_id = ?`, workspaceID, agentID))
}

func scanWorkspace(s scanner) (core.Workspace, error) {
	var w core.Workspace
	var created int64
	if err := s.Scan(&w.ID, &w.OwnerID, &w.Name, &w.LeaderID, &w.Orchestrator, &w.NextSeq, &created); err != nil {
		return w, mapErr(err)
	}
	w.CreatedAt = tm(created)
	return w, nil
}
