package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/austiecodes/solax/internal/server/model"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// AgentSpec is what a person sets up for an agent.
type AgentSpec struct {
	Machine      string // machine ID or name
	Name         string
	Description  string
	Instructions string // system prompt after the Solax role prompt
	Runtime      ir.Kind
	Dir          string
	Model        string
	Options      map[string]any
	Super        bool // the Super Agent: the router that talks to the user
}

// AgentPatch changes some fields of an agent. Nil fields stay as they are.
type AgentPatch struct {
	Description  *string
	Instructions *string
	Dir          *string
	Model        *string
	Options      map[string]any
	Super        *bool
}

// agentName is a name that a prompt and a shell can carry without quotes.
var agentName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// CreateAgent sets up an agent on a machine. A built-in runtime does not
// have to be detected yet: a machine may be offline while a person sets it
// up. Any other runtime (a fork of Claude Code from the config of a daemon)
// must be one that the machine reported.
// There is at most one Super Agent; a new one replaces the role of the old.
func CreateAgent(ctx context.Context, a model.Actor, spec AgentSpec) (model.Agent, error) {
	if err := requireHuman(a); err != nil {
		return model.Agent{}, err
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if !agentName.MatchString(spec.Name) {
		return model.Agent{}, fmt.Errorf("%w: agent name must match %s", errs.ErrInvalid, agentName)
	}
	if !agentName.MatchString(string(spec.Runtime)) {
		return model.Agent{}, fmt.Errorf("%w: runtime must be one of %s, or one that the machine reports", errs.ErrInvalid, kindList())
	}
	if strings.TrimSpace(spec.Dir) == "" {
		return model.Agent{}, fmt.Errorf("%w: an agent needs a work folder (dir)", errs.ErrInvalid)
	}
	var ag model.Agent
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		m, err := resolveMachine(ctx, tx, a, spec.Machine)
		if err != nil {
			return err
		}
		if !slices.Contains(ir.BuiltinKinds, spec.Runtime) && !m.Reports(spec.Runtime) {
			return fmt.Errorf("%w: runtime %q: machine %s does not report it; the built-in runtimes are %s",
				errs.ErrInvalid, spec.Runtime, m.Name, kindList())
		}
		if _, err := model.GetAgentByName(ctx, tx, spec.Name); err == nil {
			return fmt.Errorf("%w: agent %q exists", errs.ErrConflict, spec.Name)
		} else if !errors.Is(err, errs.ErrNotFound) {
			return err
		}
		now := clock()
		ag = model.Agent{
			ID: model.NewIDAt(model.PrefixAgent, now), MachineID: m.ID, Name: spec.Name, Description: strings.TrimSpace(spec.Description),
			Instructions: strings.TrimSpace(spec.Instructions), Runtime: spec.Runtime, Dir: spec.Dir, Model: spec.Model, Options: spec.Options, CreatedAt: now,
		}
		if spec.Super {
			if err := clearSuper(ctx, tx); err != nil {
				return err
			}
			ag.Role = model.RoleSuper
		}
		return model.SaveAgent(ctx, tx, ag)
	})
	if err == nil {
		Publish(ctx, a.OwnerID, PushAgents, nil)
	}
	return ag, err
}

// UpdateAgent changes an agent. Running sessions keep their settings
// until they restart.
func UpdateAgent(ctx context.Context, a model.Actor, ref string, p AgentPatch) (model.Agent, error) {
	if err := requireHuman(a); err != nil {
		return model.Agent{}, err
	}
	var ag model.Agent
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		var err error
		if ag, err = resolveAgentRef(ctx, tx, a, ref); err != nil {
			return err
		}
		if p.Description != nil {
			ag.Description = strings.TrimSpace(*p.Description)
		}
		if p.Instructions != nil {
			ag.Instructions = strings.TrimSpace(*p.Instructions)
		}
		if p.Dir != nil {
			if strings.TrimSpace(*p.Dir) == "" {
				return fmt.Errorf("%w: dir cannot be empty", errs.ErrInvalid)
			}
			ag.Dir = *p.Dir
		}
		if p.Model != nil {
			ag.Model = *p.Model
		}
		if p.Options != nil {
			ag.Options = p.Options
		}
		if p.Super != nil {
			if *p.Super && ag.Role != model.RoleSuper {
				if err := clearSuper(ctx, tx); err != nil {
					return err
				}
				ag.Role = model.RoleSuper
			} else if !*p.Super {
				ag.Role = model.RoleWorker
			}
		}
		return model.SaveAgent(ctx, tx, ag)
	})
	if err == nil {
		Publish(ctx, a.OwnerID, PushAgents, nil)
	}
	return ag, err
}

// DeleteAgent removes an agent. Its jobs and session history stay.
func DeleteAgent(ctx context.Context, a model.Actor, ref string) error {
	if err := requireHuman(a); err != nil {
		return err
	}
	err := model.Atomic(ctx, func(tx *sql.Tx) error {
		ag, err := resolveAgentRef(ctx, tx, a, ref)
		if err != nil {
			return err
		}
		ag.Deleted = true
		return model.SaveAgent(ctx, tx, ag)
	})
	if err == nil {
		Publish(ctx, a.OwnerID, PushAgents, nil)
	}
	return err
}

// kindList is "pi, codex or claude".
func kindList() string {
	names := make([]string, len(ir.BuiltinKinds))
	for i, k := range ir.BuiltinKinds {
		names[i] = string(k)
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

func clearSuper(ctx context.Context, tx *sql.Tx) error {
	all, err := model.ListAgents(ctx, tx)
	if err != nil {
		return err
	}
	for _, old := range all {
		if old.Role == model.RoleSuper {
			old.Role = model.RoleWorker
			if err := model.SaveAgent(ctx, tx, old); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveMachine finds a machine of the actor by ID or by name.
func resolveMachine(ctx context.Context, db model.Querier, a model.Actor, ref string) (model.Machine, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return model.Machine{}, fmt.Errorf("%w: machine is required", errs.ErrInvalid)
	}
	if model.ID(ref).Prefix() == model.PrefixMachine {
		m, err := model.GetMachine(ctx, db, model.ID(ref))
		if err != nil {
			return model.Machine{}, err
		}
		return m, owned(a, m.OwnerID)
	}
	all, err := model.ListMachines(ctx, db, a.OwnerID)
	if err != nil {
		return model.Machine{}, err
	}
	var found []model.Machine
	for _, m := range all {
		if m.Name == ref && m.State != model.MachineRevoked {
			found = append(found, m)
		}
	}
	switch len(found) {
	case 0:
		return model.Machine{}, fmt.Errorf("machine %q: %w", ref, errs.ErrNotFound)
	case 1:
		return found[0], nil
	}
	return model.Machine{}, fmt.Errorf("%w: machine name %q is not unique; use its ID", errs.ErrInvalid, ref)
}

// resolveAgentRef finds a live agent by ID or by name.
func resolveAgentRef(ctx context.Context, db model.Querier, a model.Actor, ref string) (model.Agent, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "@")
	var (
		ag  model.Agent
		err error
	)
	if model.ID(ref).Prefix() == model.PrefixAgent {
		ag, err = model.GetAgent(ctx, db, model.ID(ref))
		if err == nil && ag.Deleted {
			err = errs.ErrNotFound
		}
	} else {
		ag, err = model.GetAgentByName(ctx, db, ref)
	}
	if err != nil {
		return model.Agent{}, fmt.Errorf("agent %q: %w", ref, err)
	}
	m, err := model.GetMachine(ctx, db, ag.MachineID)
	if err != nil {
		return model.Agent{}, err
	}
	if err := owned(a, m.OwnerID); err != nil {
		return model.Agent{}, fmt.Errorf("agent %q: %w", ref, err)
	}
	return ag, nil
}

// superAgent returns the Super Agent, or ErrNotFound.
func superAgent(ctx context.Context, db model.Querier) (model.Agent, error) {
	all, err := model.ListAgents(ctx, db)
	if err != nil {
		return model.Agent{}, err
	}
	for _, ag := range all {
		if ag.Role == model.RoleSuper {
			return ag, nil
		}
	}
	return model.Agent{}, fmt.Errorf("no Super Agent is set up: %w", errs.ErrNotFound)
}
