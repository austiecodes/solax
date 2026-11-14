package api

import (
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Mapping from core values to wire DTOs. Handlers never expose core types.

func machineDTO(m core.Machine) protocol.Machine {
	drivers := m.Drivers
	if drivers == nil {
		drivers = []protocol.DriverProbe{}
	}
	return protocol.Machine{ID: string(m.ID), Name: m.Name, State: protocol.MachineState(m.State),
		Online: m.Online, LastSeen: m.LastSeen, Drivers: drivers}
}

func enrolledDTO(m core.Machine) protocol.MachineEnrolled {
	return protocol.MachineEnrolled{MachineID: string(m.ID), State: protocol.MachineState(m.State),
		PairCode: m.PairCode, PairUntil: m.PairUntil}
}

func agentDTO(v core.AgentView) protocol.Agent {
	return protocol.Agent{
		ID: string(v.ID), Name: v.Name, Description: v.Description, MachineID: string(v.MachineID), Machine: v.MachineName,
		Online: v.Online, Driver: v.Driver, Role: string(v.Role), Dir: v.Dir, Model: v.Model, Options: v.Options,
		SessionID: string(v.SessionID),
	}
}

func workspaceDTO(w core.Workspace, members []core.Member) protocol.Workspace {
	out := protocol.Workspace{ID: string(w.ID), Name: w.Name, Leader: string(w.LeaderID), Members: []protocol.Member{}}
	for _, m := range members {
		out.Members = append(out.Members, protocol.Member{AgentID: string(m.AgentID), Role: string(m.Role)})
	}
	return out
}

func taskDTO(t core.Task) protocol.Task {
	return protocol.Task{
		ID: string(t.ID), WorkspaceID: string(t.WorkspaceID), ParentID: string(t.ParentID), Depth: t.Depth,
		Title: t.Title, Brief: t.Brief, General: t.General, State: string(t.State),
		BlockedReason: string(t.BlockedReason), Orchestrator: t.Orchestrator,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func messageDTO(m core.Message) protocol.Message {
	out := protocol.Message{
		ID: string(m.ID), WorkspaceID: string(m.WorkspaceID), TaskID: string(m.TaskID), Seq: m.Seq,
		Author: protocol.Author{Kind: string(m.Author.Kind), ID: string(m.Author.ID)},
		Kind:   string(m.Kind), Status: string(m.Status), Text: m.Text, CreatedAt: m.CreatedAt,
	}
	for _, id := range m.Mentions {
		out.Mentions = append(out.Mentions, string(id))
	}
	return out
}

// mapAll maps a slice; it never returns nil, so JSON is [] and not null.
func mapAll[T, U any](in []T, f func(T) U) []U {
	out := make([]U, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}
