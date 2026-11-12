package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
)

// workspaceName is a short, CLI-friendly name. It cannot look like an ID.
var workspaceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// GeneralBudget is the budget of the general task of a workspace. It has no
// lifecycle, so only the loop guard applies.
var GeneralBudget = Budget{MaxHopsWithoutHuman: DefaultBudget.MaxHopsWithoutHuman}

// CreateWorkspace creates a workspace with its general task. leader is an
// agent reference (see resolveAgent); it may be empty, and then the first
// member added becomes the leader.
func (e *Engine) CreateWorkspace(ctx context.Context, a Actor, name, leader string) (Workspace, error) {
	if err := requireManager(a); err != nil {
		return Workspace{}, err
	}
	name = strings.TrimSpace(name)
	if !workspaceName.MatchString(name) || ID(name).Prefix() == PrefixWorkspace {
		return Workspace{}, fmt.Errorf("%w: workspace name must match %s", errs.ErrInvalid, workspaceName)
	}
	var w Workspace
	err := e.store.Atomic(ctx, func(tx Tx) error {
		if _, err := tx.Workspaces().ByName(ctx, a.OwnerID, name); err == nil {
			return fmt.Errorf("%w: workspace %q exists", errs.ErrConflict, name)
		} else if !errors.Is(err, errs.ErrNotFound) {
			return err
		}
		now := e.clock()
		w = Workspace{ID: NewIDAt(PrefixWorkspace, now), OwnerID: a.OwnerID, Name: name, CreatedAt: now}
		if leader != "" {
			ag, err := resolveAgent(ctx, tx, a.OwnerID, leader)
			if err != nil {
				return err
			}
			w.LeaderID = ag.ID
		}
		if err := tx.Workspaces().Save(ctx, w); err != nil {
			return err
		}
		if w.LeaderID != "" {
			if err := tx.Workspaces().SaveMember(ctx, Member{WorkspaceID: w.ID, AgentID: w.LeaderID, Role: MemberLeader}); err != nil {
				return err
			}
		}
		return tx.Tasks().Create(ctx, Task{
			ID: NewIDAt(PrefixTask, now), WorkspaceID: w.ID, Title: "general", General: true,
			State: TaskRunning, Budget: GeneralBudget, CreatedAt: now, UpdatedAt: now,
		})
	})
	return w, err
}

// AddMember adds an agent to a workspace, or changes its role. With leader
// set, or when the workspace has no leader yet, the agent becomes the leader
// and the old leader becomes a plain member.
func (e *Engine) AddMember(ctx context.Context, a Actor, ws ID, agentRef string, leader bool) error {
	if err := requireManager(a); err != nil {
		return err
	}
	return e.store.Atomic(ctx, func(tx Tx) error {
		w, err := resolveWorkspace(ctx, tx, a, ws)
		if err != nil {
			return err
		}
		ag, err := resolveAgent(ctx, tx, a.OwnerID, agentRef)
		if err != nil {
			return err
		}
		role := MemberPlain
		if leader || w.LeaderID == "" || w.LeaderID == ag.ID {
			role = MemberLeader
		}
		if role == MemberLeader && w.LeaderID != "" && w.LeaderID != ag.ID {
			if err := tx.Workspaces().SaveMember(ctx, Member{WorkspaceID: w.ID, AgentID: w.LeaderID, Role: MemberPlain}); err != nil {
				return err
			}
		}
		if role == MemberLeader && w.LeaderID != ag.ID {
			w.LeaderID = ag.ID
			if err := tx.Workspaces().Save(ctx, w); err != nil {
				return err
			}
		}
		return tx.Workspaces().SaveMember(ctx, Member{WorkspaceID: w.ID, AgentID: ag.ID, Role: role})
	})
}

// RemoveMember removes an agent from a workspace. The leader can only leave
// last: make another member the leader first.
func (e *Engine) RemoveMember(ctx context.Context, a Actor, ws ID, agentRef string) error {
	if err := requireManager(a); err != nil {
		return err
	}
	return e.store.Atomic(ctx, func(tx Tx) error {
		w, err := resolveWorkspace(ctx, tx, a, ws)
		if err != nil {
			return err
		}
		ag, err := resolveAgent(ctx, tx, a.OwnerID, agentRef)
		if err != nil {
			return err
		}
		if ag.ID == w.LeaderID {
			members, err := tx.Workspaces().Members(ctx, w.ID)
			if err != nil {
				return err
			}
			if len(members) > 1 {
				return fmt.Errorf("%w: make another member the leader first", errs.ErrConflict)
			}
			w.LeaderID = ""
			if err := tx.Workspaces().Save(ctx, w); err != nil {
				return err
			}
		}
		return tx.Workspaces().DeleteMember(ctx, w.ID, ag.ID)
	})
}

// resolveWorkspace finds a workspace by ID or name, checks the owner and the
// scope of a worker agent.
func resolveWorkspace(ctx context.Context, tx Tx, a Actor, ref ID) (Workspace, error) {
	var w Workspace
	var err error
	if ref.Prefix() == PrefixWorkspace {
		w, err = tx.Workspaces().Get(ctx, ref)
	} else {
		w, err = tx.Workspaces().ByName(ctx, a.OwnerID, string(ref))
	}
	if err != nil {
		return w, err
	}
	if err := owned(a, w.OwnerID); err != nil {
		return w, err
	}
	return w, checkScope(a, w.ID, "")
}

// resolveAgent finds a worker agent of an owner by reference:
//
//   - an agent ID ("agt_...");
//   - "name@machine", with the machine name;
//   - "name", when exactly one machine of the owner has that agent.
//
// The Super Agent is never a workspace member.
func resolveAgent(ctx context.Context, tx Tx, ownerID ID, ref string) (Agent, error) {
	ref = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ref), "@"))
	if ref == "" {
		return Agent{}, fmt.Errorf("%w: empty agent reference", errs.ErrInvalid)
	}
	machines, err := tx.Machines().List(ctx)
	if err != nil {
		return Agent{}, err
	}
	mine := map[ID]Machine{}
	for _, m := range machines {
		if m.OwnerID == ownerID {
			mine[m.ID] = m
		}
	}
	var found []Agent
	if ID(ref).Prefix() == PrefixAgent {
		ag, err := tx.Agents().Get(ctx, ID(ref))
		if err != nil {
			return Agent{}, err
		}
		found = append(found, ag)
	} else {
		name, machine, qualified := strings.Cut(ref, "@")
		all, err := tx.Agents().List(ctx)
		if err != nil {
			return Agent{}, err
		}
		for _, ag := range all {
			m, ok := mine[ag.MachineID]
			if ok && ag.Name == name && (!qualified || m.Name == machine) {
				found = append(found, ag)
			}
		}
	}
	if len(found) == 1 {
		if _, ok := mine[found[0].MachineID]; !ok {
			return Agent{}, errs.ErrNotFound
		}
	}
	switch {
	case len(found) == 0:
		return Agent{}, fmt.Errorf("%w: agent %q", errs.ErrNotFound, ref)
	case len(found) > 1:
		return Agent{}, fmt.Errorf("%w: agent %q is on several machines; use name@machine", errs.ErrInvalid, ref)
	case found[0].Role == RoleSuper:
		return Agent{}, fmt.Errorf("%w: the Super Agent cannot join a workspace", errs.ErrInvalid)
	}
	return found[0], nil
}
