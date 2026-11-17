package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/server/model"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Queries run in read-only snapshot transactions. Objects of other owners
// look absent.

// Machines returns the machines of the owner.
func Machines(ctx context.Context, a model.Actor) ([]model.Machine, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	return model.ListMachines(ctx, model.ReadDB, a.OwnerID)
}

// AgentView is an agent with the state of its machine.
type AgentView struct {
	model.Agent
	MachineName string
	// Online is true when the machine is connected and its daemon detected
	// a working runtime for the agent.
	Online bool
	// SessionID is the conversation session of the Super Agent, once it
	// has one. It is empty for other agents: their sessions are per job.
	SessionID model.ID
}

// Agents lists the live agents of the owner. The Super Agent uses it to
// choose where a job goes.
func Agents(ctx context.Context, a model.Actor) ([]AgentView, error) {
	var out []AgentView
	err := model.Snapshot(ctx, func(tx *sql.Tx) error {
		machines, err := model.ListMachines(ctx, tx, a.OwnerID)
		if err != nil {
			return err
		}
		byID := map[model.ID]model.Machine{}
		for _, m := range machines {
			byID[m.ID] = m
		}
		all, err := model.ListAgents(ctx, tx)
		for _, ag := range all {
			m, ok := byID[ag.MachineID]
			if !ok {
				continue
			}
			v := AgentView{Agent: ag, MachineName: m.Name, Online: m.Online && m.HasRuntime(ag.Runtime)}
			if ag.Role == model.RoleSuper {
				if ses, err := model.GetSessionFor(ctx, tx, ag.ID, ""); err == nil {
					v.SessionID = ses.ID
				}
			}
			out = append(out, v)
		}
		return err
	})
	return out, err
}

// SuperMessages returns the conversation of the user with the Super
// Agent, with seq > since.
func SuperMessages(ctx context.Context, a model.Actor, since int64, limit int) ([]model.Message, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	return model.ListMessages(ctx, model.ReadDB, a.OwnerID, since, limit)
}

// Recall returns any object by ID with the IDs of related objects. The
// summary is short on purpose: the caller follows links for more.
func Recall(ctx context.Context, a model.Actor, id model.ID) (protocol.Object, error) {
	var obj protocol.Object
	err := model.Snapshot(ctx, func(tx *sql.Tx) error {
		var err error
		obj, err = recall(ctx, tx, a, id)
		return err
	})
	return obj, err
}

func recall(ctx context.Context, db model.Querier, a model.Actor, id model.ID) (protocol.Object, error) {
	obj := protocol.Object{ID: string(id), Links: map[string]string{}}
	switch id.Prefix() {
	case model.PrefixMessage:
		m, err := model.GetMessage(ctx, db, id)
		if err != nil {
			return obj, err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return obj, err
		}
		if err := requireManager(a); err != nil {
			return obj, errs.ErrNotFound
		}
		obj.Type = "message"
		obj.Summary = fmt.Sprintf("%s by %s:%s: %s", m.Kind, m.Author.Kind, m.Author.ID, clip(m.Text, 600))
		if m.Author.ID != "" {
			obj.Links["author"] = string(m.Author.ID)
		}
	case model.PrefixAgent:
		ag, err := model.GetAgent(ctx, db, id)
		if err != nil {
			return obj, err
		}
		m, err := model.GetMachine(ctx, db, ag.MachineID)
		if err != nil {
			return obj, err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return obj, err
		}
		obj.Type = "agent"
		obj.Summary = fmt.Sprintf("Agent %s@%s (%s, %s).", ag.Name, m.Name, ag.Runtime, onlineWord(m.Online))
		obj.Links["machine"] = string(m.ID)
	case model.PrefixMachine:
		m, err := model.GetMachine(ctx, db, id)
		if err != nil {
			return obj, err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return obj, err
		}
		obj.Type = "machine"
		obj.Summary = fmt.Sprintf("Machine %q (%s, %s, %d free slots).", m.Name, m.State, onlineWord(m.Online), m.Slots)
	default:
		return obj, fmt.Errorf("%w: cannot recall %q", errs.ErrNotFound, id)
	}
	if len(obj.Links) == 0 {
		obj.Links = nil
	}
	return obj, nil
}

func onlineWord(on bool) string {
	if on {
		return "online"
	}
	return "offline"
}

// clip shortens s to n runes with an ellipsis.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
