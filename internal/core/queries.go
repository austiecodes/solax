package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Queries run in read-only snapshot transactions. Objects of other owners
// look absent; a worker agent sees only its own workspace (and task).

func (e *Engine) Machines(ctx context.Context, a Actor) ([]Machine, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	var out []Machine
	err := e.store.Read(ctx, func(tx Tx) error {
		all, err := tx.Machines().List(ctx)
		for _, m := range all {
			if m.OwnerID == a.OwnerID {
				out = append(out, m)
			}
		}
		return err
	})
	return out, err
}

func (e *Engine) Agents(ctx context.Context, a Actor) ([]Agent, error) {
	var out []Agent
	err := e.store.Read(ctx, func(tx Tx) error {
		machines, err := tx.Machines().List(ctx)
		if err != nil {
			return err
		}
		mine := map[ID]bool{}
		for _, m := range machines {
			mine[m.ID] = m.OwnerID == a.OwnerID
		}
		all, err := tx.Agents().List(ctx)
		for _, ag := range all {
			if mine[ag.MachineID] {
				out = append(out, ag)
			}
		}
		return err
	})
	return out, err
}

func (e *Engine) Workspaces(ctx context.Context, a Actor) ([]Workspace, error) {
	var out []Workspace
	err := e.store.Read(ctx, func(tx Tx) error {
		all, err := tx.Workspaces().List(ctx)
		for _, w := range all {
			if w.OwnerID == a.OwnerID && checkScope(a, w.ID, "") == nil {
				out = append(out, w)
			}
		}
		return err
	})
	return out, err
}

func (e *Engine) Workspace(ctx context.Context, a Actor, ws ID) (Workspace, []Member, error) {
	var w Workspace
	var members []Member
	err := e.store.Read(ctx, func(tx Tx) error {
		var err error
		if w, err = resolveWorkspace(ctx, tx, a, ws); err != nil {
			return err
		}
		members, err = tx.Workspaces().Members(ctx, w.ID)
		return err
	})
	return w, members, err
}

func (e *Engine) Tasks(ctx context.Context, a Actor, ws ID) ([]Task, error) {
	var out []Task
	err := e.store.Read(ctx, func(tx Tx) error {
		w, err := resolveWorkspace(ctx, tx, a, ws)
		if err != nil {
			return err
		}
		out, err = tx.Tasks().List(ctx, w.ID)
		return err
	})
	return out, err
}

func (e *Engine) Task(ctx context.Context, a Actor, task ID) (Task, error) {
	var t Task
	err := e.store.Read(ctx, func(tx Tx) error {
		var err error
		t, err = readableTask(ctx, tx, a, task)
		return err
	})
	return t, err
}

func (e *Engine) Messages(ctx context.Context, a Actor, task ID, since int64, limit int) ([]Message, error) {
	var out []Message
	err := e.store.Read(ctx, func(tx Tx) error {
		t, err := readableTask(ctx, tx, a, task)
		if err != nil {
			return err
		}
		out, err = tx.Messages().Since(ctx, t.ID, since, limit)
		return err
	})
	return out, err
}

func (e *Engine) SuperMessages(ctx context.Context, a Actor, since int64, limit int) ([]Message, error) {
	if err := requireManager(a); err != nil {
		return nil, err
	}
	var out []Message
	err := e.store.Read(ctx, func(tx Tx) error {
		var err error
		out, err = tx.Messages().SuperSince(ctx, a.OwnerID, since, limit)
		return err
	})
	return out, err
}

// Recall returns any object by ID with the IDs of related objects (Chain of
// Recall). The summary is short on purpose: the caller follows links for more.
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
	case PrefixWorkspace:
		w, err := resolveWorkspace(ctx, tx, a, id)
		if err != nil {
			return obj, err
		}
		members, err := tx.Workspaces().Members(ctx, w.ID)
		if err != nil {
			return obj, err
		}
		open, err := tx.Tasks().Open(ctx, w.ID)
		if err != nil {
			return obj, err
		}
		obj.Type = "workspace"
		obj.Summary = fmt.Sprintf("Workspace %q: %d members, %d open tasks.", w.Name, len(members), len(open))
		if w.LeaderID != "" {
			obj.Links["leader"] = string(w.LeaderID)
		}
		for i, t := range open {
			obj.Links[fmt.Sprintf("task.%d", i)] = string(t.ID)
		}
	case PrefixTask:
		t, err := readableTask(ctx, tx, a, id)
		if err != nil {
			return obj, err
		}
		obj.Type = "task"
		obj.Summary = fmt.Sprintf("Task %q (%s): %s", t.Title, t.State, clip(t.Brief, 400))
		obj.Links["workspace"] = string(t.WorkspaceID)
		if t.ParentID != "" {
			obj.Links["parent"] = string(t.ParentID)
		}
		kids, err := tx.Tasks().Children(ctx, t.ID)
		if err != nil {
			return obj, err
		}
		for i, k := range kids {
			obj.Links[fmt.Sprintf("child.%d", i)] = string(k.ID)
		}
	case PrefixMessage:
		m, err := tx.Messages().Get(ctx, id)
		if err != nil {
			return obj, err
		}
		if m.WorkspaceID == "" { // the Super Agent conversation
			if err := owned(a, m.OwnerID); err != nil {
				return obj, err
			}
			if err := requireManager(a); err != nil {
				return obj, errs.ErrNotFound
			}
		} else if _, err := readableTask(ctx, tx, a, m.TaskID); err != nil {
			return obj, err
		}
		obj.Type = "message"
		obj.Summary = fmt.Sprintf("%s %s by %s:%s: %s", m.Kind, m.Status, m.Author.Kind, m.Author.ID, clip(m.Text, 600))
		if m.TaskID != "" {
			obj.Links["task"] = string(m.TaskID)
		}
		if m.Author.ID != "" {
			obj.Links["author"] = string(m.Author.ID)
		}
	case PrefixAgent:
		ag, err := tx.Agents().Get(ctx, id)
		if err != nil {
			return obj, err
		}
		m, err := tx.Machines().Get(ctx, ag.MachineID)
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
		m, err := tx.Machines().Get(ctx, id)
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

// readableTask loads a task the actor may read.
func readableTask(ctx context.Context, tx Tx, a Actor, id ID) (Task, error) {
	t, err := tx.Tasks().Get(ctx, id)
	if err != nil {
		return t, err
	}
	w, err := tx.Workspaces().Get(ctx, t.WorkspaceID)
	if err != nil {
		return t, err
	}
	if err := owned(a, w.OwnerID); err != nil {
		return t, err
	}
	if err := checkScope(a, t.WorkspaceID, t.ID); err != nil {
		return t, err
	}
	return t, nil
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
