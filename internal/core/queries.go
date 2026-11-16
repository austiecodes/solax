package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Queries run in read-only snapshot transactions. Objects of other owners
// look absent.

// Machines returns the machines of the owner.
func (e *Engine) Machines(ctx context.Context, a Actor) ([]Machine, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	var out []Machine
	err := e.store.Read(ctx, func(tx Tx) error {
		all, err := tx.Machines(ctx)
		for _, m := range all {
			if m.OwnerID == a.OwnerID {
				out = append(out, m)
			}
		}
		return err
	})
	return out, err
}

// AgentView is an agent with the state of its machine.
type AgentView struct {
	Agent
	MachineName string
	// Online is true when the machine is connected and its daemon detected
	// a working driver for the agent.
	Online bool
	// SessionID is the conversation session of the Super Agent, once it
	// has one. It is empty for other agents: their sessions are per job.
	SessionID ID
}

// Agents lists the live agents of the owner. The Super Agent uses it to
// choose where a job goes.
func (e *Engine) Agents(ctx context.Context, a Actor) ([]AgentView, error) {
	var out []AgentView
	err := e.store.Read(ctx, func(tx Tx) error {
		machines, err := tx.Machines(ctx)
		if err != nil {
			return err
		}
		byID := map[ID]Machine{}
		for _, m := range machines {
			if m.OwnerID == a.OwnerID {
				byID[m.ID] = m
			}
		}
		all, err := tx.Agents(ctx)
		for _, ag := range all {
			m, ok := byID[ag.MachineID]
			if !ok {
				continue
			}
			v := AgentView{Agent: ag, MachineName: m.Name, Online: m.Online && m.HasDriver(ag.Driver)}
			if ag.Role == RoleSuper {
				if ses, err := tx.SessionFor(ctx, ag.ID, ""); err == nil {
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
func (e *Engine) SuperMessages(ctx context.Context, a Actor, since int64, limit int) ([]Message, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	var out []Message
	err := e.store.Read(ctx, func(tx Tx) error {
		var err error
		out, err = tx.Messages(ctx, a.OwnerID, since, limit)
		return err
	})
	return out, err
}

// Recall returns any object by ID with the IDs of related objects. The
// summary is short on purpose: the caller follows links for more.
func (e *Engine) Recall(ctx context.Context, a Actor, id ID) (protocol.Object, error) {
	var obj protocol.Object
	err := e.store.Read(ctx, func(tx Tx) error {
		var err error
		obj, err = recall(ctx, tx, a, id)
		return err
	})
	return obj, err
}

func recall(ctx context.Context, tx Tx, a Actor, id ID) (protocol.Object, error) {
	obj := protocol.Object{ID: string(id), Links: map[string]string{}}
	switch id.Prefix() {
	case PrefixMessage:
		m, err := tx.Message(ctx, id)
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
	case PrefixAgent:
		ag, err := tx.Agent(ctx, id)
		if err != nil {
			return obj, err
		}
		m, err := tx.Machine(ctx, ag.MachineID)
		if err != nil {
			return obj, err
		}
		if err := owned(a, m.OwnerID); err != nil {
			return obj, err
		}
		obj.Type = "agent"
		obj.Summary = fmt.Sprintf("Agent %s@%s (%s, %s).", ag.Name, m.Name, ag.Driver, onlineWord(m.Online))
		obj.Links["machine"] = string(m.ID)
	case PrefixMachine:
		m, err := tx.Machine(ctx, id)
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
