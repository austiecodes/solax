package api

import (
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Wire forms of model values. Handlers never expose model types.

func wireMachine(m model.Machine) protocol.Machine {
	runtimes := m.Runtimes
	if runtimes == nil {
		runtimes = []protocol.RuntimeProbe{}
	}
	return protocol.Machine{ID: string(m.ID), Name: m.Name, State: protocol.MachineState(m.State),
		Online: m.Online, LastSeen: m.LastSeen, Runtimes: runtimes}
}

func wireEnrolled(m model.Machine) protocol.MachineEnrolled {
	return protocol.MachineEnrolled{MachineID: string(m.ID), State: protocol.MachineState(m.State),
		PairCode: m.PairCode, PairUntil: m.PairUntil}
}

func wireAgent(v service.AgentView) protocol.Agent {
	return protocol.Agent{
		ID: string(v.ID), Name: v.Name, Description: v.Description, Instructions: v.Instructions, MachineID: string(v.MachineID), Machine: v.MachineName,
		Online: v.Online, Runtime: v.Runtime, Role: string(v.Role), Dir: v.Dir, Model: v.Model, Options: v.Options,
		SessionID: string(v.SessionID),
	}
}

// mapAll maps a slice; it never returns nil, so JSON is [] and not null.
func mapAll[T, U any](in []T, f func(T) U) []U {
	out := make([]U, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}
