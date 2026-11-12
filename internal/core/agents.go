package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/ir"
)

// AgentSpec is what a person sets up for an agent.
type AgentSpec struct {
	Machine     string // machine ID or name
	Name        string
	Description string
	Driver      ir.Kind
	Dir         string
	Model       string
	Options     map[string]any
	Super       bool // the Super Agent: the router that talks to the user
}

// AgentPatch changes some fields of an agent. Nil fields stay as they are.
type AgentPatch struct {
	Description *string
	Dir         *string
	Model       *string
	Options     map[string]any
	Super       *bool
}

// agentName is a name that a prompt and a shell can carry without quotes.
var agentName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

var drivers = map[ir.Kind]bool{ir.KindClaude: true, ir.KindCodex: true, ir.KindPi: true}

// CreateAgent sets up an agent on a machine. The driver does not have to
// be detected yet: a machine may be offline while a person sets it up.
// There is at most one Super Agent; a new one replaces the role of the old.
func (e *Engine) CreateAgent(ctx context.Context, a Actor, spec AgentSpec) (Agent, error) {
	if err := requireHuman(a); err != nil {
		return Agent{}, err
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if !agentName.MatchString(spec.Name) {
		return Agent{}, fmt.Errorf("%w: agent name must match %s", errs.ErrInvalid, agentName)
	}
	if !drivers[spec.Driver] {
		return Agent{}, fmt.Errorf("%w: driver must be claude, codex or pi", errs.ErrInvalid)
	}
	if strings.TrimSpace(spec.Dir) == "" {
		return Agent{}, fmt.Errorf("%w: an agent needs a work folder (dir)", errs.ErrInvalid)
	}
	var ag Agent
	err := e.store.Atomic(ctx, func(tx Tx) error {
		m, err := resolveMachine(ctx, tx, a, spec.Machine)
		if err != nil {
			return err
		}
		if _, err := tx.Agents().Named(ctx, spec.Name); err == nil {
			return fmt.Errorf("%w: agent %q exists", errs.ErrConflict, spec.Name)
		} else if !errors.Is(err, errs.ErrNotFound) {
			return err
		}
		now := e.clock()
		ag = Agent{
			ID: NewIDAt(PrefixAgent, now), MachineID: m.ID, Name: spec.Name, Description: strings.TrimSpace(spec.Description),
			Driver: spec.Driver, Dir: spec.Dir, Model: spec.Model, Options: spec.Options, CreatedAt: now,
		}
		if spec.Super {
			if err := clearSuper(ctx, tx); err != nil {
				return err
			}
			ag.Role = RoleSuper
		}
		return tx.Agents().Save(ctx, ag)
	})
	if err == nil {
		e.publish(ctx, a.OwnerID, PushAgents, nil)
	}
	return ag, err
}

// UpdateAgent changes an agent. Running sessions keep their settings
// until they restart.
func (e *Engine) UpdateAgent(ctx context.Context, a Actor, ref string, p AgentPatch) (Agent, error) {
	if err := requireHuman(a); err != nil {
		return Agent{}, err
	}
	var ag Agent
	err := e.store.Atomic(ctx, func(tx Tx) error {
		var err error
		if ag, err = resolveAgentRef(ctx, tx, a, ref); err != nil {
			return err
		}
		if p.Description != nil {
			ag.Description = strings.TrimSpace(*p.Description)
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
			if *p.Super && ag.Role != RoleSuper {
				if err := clearSuper(ctx, tx); err != nil {
					return err
				}
				ag.Role = RoleSuper
			} else if !*p.Super {
				ag.Role = RoleWorker
			}
		}
		return tx.Agents().Save(ctx, ag)
	})
	if err == nil {
		e.publish(ctx, a.OwnerID, PushAgents, nil)
	}
	return ag, err
}

// DeleteAgent removes an agent. Its jobs and session history stay.
func (e *Engine) DeleteAgent(ctx context.Context, a Actor, ref string) error {
	if err := requireHuman(a); err != nil {
		return err
	}
	err := e.store.Atomic(ctx, func(tx Tx) error {
		ag, err := resolveAgentRef(ctx, tx, a, ref)
		if err != nil {
			return err
		}
		ag.Deleted = true
		return tx.Agents().Save(ctx, ag)
	})
	if err == nil {
		e.publish(ctx, a.OwnerID, PushAgents, nil)
	}
	return err
}

func clearSuper(ctx context.Context, tx Tx) error {
	all, err := tx.Agents().List(ctx)
	if err != nil {
		return err
	}
	for _, old := range all {
		if old.Role == RoleSuper {
			old.Role = RoleWorker
			if err := tx.Agents().Save(ctx, old); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveMachine finds a machine of the actor by ID or by name.
func resolveMachine(ctx context.Context, tx Tx, a Actor, ref string) (Machine, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Machine{}, fmt.Errorf("%w: machine is required", errs.ErrInvalid)
	}
	if ID(ref).Prefix() == PrefixMachine {
		m, err := tx.Machines().Get(ctx, ID(ref))
		if err != nil {
			return Machine{}, err
		}
		return m, owned(a, m.OwnerID)
	}
	all, err := tx.Machines().List(ctx)
	if err != nil {
		return Machine{}, err
	}
	var found []Machine
	for _, m := range all {
		if m.OwnerID == a.OwnerID && m.Name == ref && m.State != MachineRevoked {
			found = append(found, m)
		}
	}
	switch len(found) {
	case 0:
		return Machine{}, fmt.Errorf("machine %q: %w", ref, errs.ErrNotFound)
	case 1:
		return found[0], nil
	}
	return Machine{}, fmt.Errorf("%w: machine name %q is not unique; use its ID", errs.ErrInvalid, ref)
}

// resolveAgentRef finds a live agent by ID or by name.
func resolveAgentRef(ctx context.Context, tx Tx, a Actor, ref string) (Agent, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "@")
	var (
		ag  Agent
		err error
	)
	if ID(ref).Prefix() == PrefixAgent {
		ag, err = tx.Agents().Get(ctx, ID(ref))
		if err == nil && ag.Deleted {
			err = errs.ErrNotFound
		}
	} else {
		ag, err = tx.Agents().Named(ctx, ref)
	}
	if err != nil {
		return Agent{}, fmt.Errorf("agent %q: %w", ref, err)
	}
	m, err := tx.Machines().Get(ctx, ag.MachineID)
	if err != nil {
		return Agent{}, err
	}
	if err := owned(a, m.OwnerID); err != nil {
		return Agent{}, fmt.Errorf("agent %q: %w", ref, err)
	}
	return ag, nil
}

// superAgent returns the Super Agent, or ErrNotFound.
func superAgent(ctx context.Context, tx Tx) (Agent, error) {
	all, err := tx.Agents().List(ctx)
	if err != nil {
		return Agent{}, err
	}
	for _, ag := range all {
		if ag.Role == RoleSuper {
			return ag, nil
		}
	}
	return Agent{}, fmt.Errorf("no Super Agent is set up: %w", errs.ErrNotFound)
}
