package model

import "github.com/austiecodes/solax/pkg/protocol"

// Wire forms of model values. The engine pushes them on the client stream,
// and the API returns them.

// WireJob converts a job to its wire form.
func WireJob(j Job, agentName string) protocol.Job {
	return protocol.Job{
		ID: string(j.ID), Agent: agentName, AgentID: string(j.AgentID), SessionID: string(j.SessionID),
		RequestedBy: string(j.RequestedBy), Title: j.Title, Prompt: j.Prompt, State: string(j.State),
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
