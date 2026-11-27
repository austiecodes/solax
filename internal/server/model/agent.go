package model

import (
	"context"
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
)

// Agent is a coding agent that a person set up on one machine. The server
// defines it; the daemon of the machine runs its sessions. The name is
// unique: the Super Agent routes tasks by name, with the description as the
// hint for what the agent is good at.
type Agent struct {
	ID          ID
	MachineID   ID
	Name        string
	Description string
	// Instructions are the system prompt of the sessions of the agent,
	// after the Solax role prompt. Empty: the role prompt only.
	Instructions string
	Runtime      ir.Kind
	Role         AgentRole
	Model        string
	Options      map[string]any // runtime-specific, for example pi "thinking"
	Deleted      bool
	CreatedAt    time.Time
}

// AgentRole is the system role of an agent.
type AgentRole string

const (
	RoleWorker AgentRole = ""
	RoleSuper  AgentRole = "super"
)

const agentCols = `id, machine_id, name, description, instructions, runtime, role, model, options, deleted, created_at`

// GetAgent returns the agent with this ID, deleted or not.
func GetAgent(ctx context.Context, db Querier, id ID) (Agent, error) {
	return scanAgent(db.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
}

// GetAgentByName returns the live (not deleted) agent with this name.
func GetAgentByName(ctx context.Context, db Querier, name string) (Agent, error) {
	return scanAgent(db.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE name = ? AND deleted = 0`, name))
}

// SaveAgent inserts or updates an agent by ID. It fails with
// errs.ErrConflict when another live agent has the same name.
func SaveAgent(ctx context.Context, db Executor, a Agent) error {
	opts, err := jsonText(a.Options)
	if err != nil {
		return err
	}
	if a.Options == nil {
		opts = "{}"
	}
	_, err = exec(ctx, db, `INSERT INTO agents(`+agentCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET machine_id = excluded.machine_id, name = excluded.name,
			description = excluded.description, instructions = excluded.instructions, runtime = excluded.runtime, role = excluded.role,
			model = excluded.model, options = excluded.options, deleted = excluded.deleted`,
		a.ID, a.MachineID, a.Name, a.Description, a.Instructions, a.Runtime, a.Role, a.Model, opts, b2i(a.Deleted), ms(a.CreatedAt))
	return err
}

// ListAgents returns the live agents, by name.
func ListAgents(ctx context.Context, db Querier) ([]Agent, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+agentCols+` FROM agents WHERE deleted = 0 ORDER BY name`)
	return collect(rows, err, scanAgent)
}

func scanAgent(s scanner) (Agent, error) {
	var a Agent
	var runtime, role, opts string
	var deleted int
	var created int64
	if err := s.Scan(&a.ID, &a.MachineID, &a.Name, &a.Description, &a.Instructions, &runtime, &role, &a.Model, &opts, &deleted, &created); err != nil {
		return a, mapErr(err)
	}
	a.Runtime, a.Role, a.Deleted, a.CreatedAt = ir.Kind(runtime), AgentRole(role), deleted != 0, tm(created)
	if opts != "" && opts != "{}" {
		if err := json.Unmarshal([]byte(opts), &a.Options); err != nil {
			return a, err
		}
	}
	return a, nil
}
