package sqlite

import (
	"context"
	"encoding/json"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/pkg/ir"
)

const agentCols = `id, machine_id, name, description, driver, role, dir, model, options, deleted, created_at`

func (t *txn) Agent(ctx context.Context, id core.ID) (core.Agent, error) {
	return scanAgent(t.q.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
}

func (t *txn) AgentByName(ctx context.Context, name string) (core.Agent, error) {
	return scanAgent(t.q.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE name = ? AND deleted = 0`, name))
}

// Save inserts or updates an agent by ID. A second live agent with the
// same name is a conflict.
func (t *txn) SaveAgent(ctx context.Context, a core.Agent) error {
	opts, err := jsonText(a.Options)
	if err != nil {
		return err
	}
	if a.Options == nil {
		opts = "{}"
	}
	_, err = t.exec(ctx, `INSERT INTO agents(`+agentCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET machine_id = excluded.machine_id, name = excluded.name,
			description = excluded.description, driver = excluded.driver, role = excluded.role, dir = excluded.dir,
			model = excluded.model, options = excluded.options, deleted = excluded.deleted`,
		a.ID, a.MachineID, a.Name, a.Description, a.Driver, a.Role, a.Dir, a.Model, opts, b2i(a.Deleted), ms(a.CreatedAt))
	return err
}

func (t *txn) Agents(ctx context.Context) ([]core.Agent, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+agentCols+` FROM agents WHERE deleted = 0 ORDER BY name`)
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
