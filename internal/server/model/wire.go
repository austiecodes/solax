package model

import (
	"encoding/json"

	"github.com/austiecodes/solax/pkg/protocol"
)

// Wire forms of model values. The service pushes them on the client stream,
// and the API returns them.

// WireTask converts a task to its wire form.
func WireTask(j Task, agentName string) protocol.Task {
	return protocol.Task{
		ID: string(j.ID), Agent: agentName, AgentID: string(j.AgentID), SessionID: string(j.SessionID),
		RequestedBy: string(j.RequestedBy), TurnBy: string(j.TurnBy), Title: j.Title, Prompt: j.Prompt, State: string(j.State),
		Result: j.Result, Error: j.Error, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
}

// WireMessage converts a conversation message to its wire form.
func WireMessage(m Message) protocol.Message {
	return protocol.Message{
		ID: string(m.ID), Seq: m.Seq, Author: protocol.Author{Kind: string(m.Author.Kind), ID: string(m.Author.ID)},
		Kind: string(m.Kind), Text: m.Text, CreatedAt: m.CreatedAt,
	}
}

// WireWorkflow converts a workflow to its wire form. full adds the source
// and steps.
func WireWorkflow(w Workflow, steps []WorkflowStep, full bool) protocol.Workflow {
	out := protocol.Workflow{
		ID: string(w.ID), Name: w.Name, Title: w.Title, RequestedBy: string(w.RequestedBy), ResumeFrom: string(w.ResumeFrom),
		State: string(w.State), Error: w.Error, Logs: w.Logs, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
	}
	if w.Result != "" {
		out.Result = json.RawMessage(w.Result)
	}
	if full {
		out.Source = w.Source
		for _, s := range steps {
			ws := protocol.WorkflowStep{Key: s.Key, Kind: s.Kind, Target: s.TargetID, Phase: s.Phase, Label: s.Label, State: string(s.State)}
			if s.OutputJSON != "" {
				ws.Output = json.RawMessage(s.OutputJSON)
			}
			out.Steps = append(out.Steps, ws)
		}
	}
	return out
}

// WireSaved converts a saved workflow to its wire form. full adds the
// source.
func WireSaved(s SavedWorkflow, full bool) protocol.SavedWorkflow {
	out := protocol.SavedWorkflow{Name: s.Name, Description: s.Description, UpdatedAt: s.UpdatedAt}
	_ = json.Unmarshal([]byte(s.MetaJSON), &out.Meta)
	if full {
		out.Source = s.Source
	}
	return out
}
