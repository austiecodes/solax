// Package protocol defines the wire types between the server, the daemon,
// the CLI and the desktop client.
package protocol

import (
	"encoding/json"

	"github.com/austiecodes/solax/pkg/ir"
)

// DaemonPath is the WebSocket endpoint that daemons dial.
const DaemonPath = "/v1/daemon"

// Envelope is one record on the daemon WebSocket. Commands carry an ID.
// The peer answers each command with an Ack that repeats the ID.
type Envelope struct {
	ID      string          `json:"id,omitempty"`
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// MessageType is the type of an Envelope.
type MessageType string

const (
	// Daemon to server.
	TypeHello        MessageType = "hello"
	TypeHeartbeat    MessageType = "heartbeat"
	TypeSessionState MessageType = "session.state"
	TypeSessionEvent MessageType = "session.event"

	// Server to daemon.
	TypeSessionEnsure  MessageType = "session.ensure"
	TypeSessionDeliver MessageType = "session.deliver"
	TypeSessionResolve MessageType = "session.resolve"
	TypeSessionStop    MessageType = "session.stop"

	// Both directions.
	TypeAck MessageType = "ack"
)

// Hello is the first record that a daemon sends after it connects.
type Hello struct {
	MachineID     string        `json:"machine_id"`
	MachineName   string        `json:"machine_name"`
	DaemonVersion string        `json:"daemon_version"`
	Drivers       []DriverProbe `json:"drivers"`
	Agents        []AgentInfo   `json:"agents"`
	FreeSlots     int           `json:"free_slots"`
}

// DriverProbe is the result of a driver probe on the machine.
type DriverProbe struct {
	Kind    ir.Kind `json:"kind"`
	Version string  `json:"version"`
	OK      bool    `json:"ok"`
	Problem string  `json:"problem,omitempty"`
	Caps    ir.Caps `json:"caps"`
}

// AgentInfo is the part of a local agent definition that the daemon
// reports. The definition itself stays in the local config of the machine.
type AgentInfo struct {
	Name   string  `json:"name"`
	Driver ir.Kind `json:"driver"`
	Role   string  `json:"role,omitempty"` // "" or "super"
	Dir    string  `json:"dir"`
	Model  string  `json:"model,omitempty"`
}

// Heartbeat is sent every 15 seconds.
type Heartbeat struct {
	FreeSlots int                        `json:"free_slots"`
	Sessions  map[string]ir.SessionState `json:"sessions"`
}

// SessionRole is the role of the agent in this session. The server decides
// it: a worker is the leader or a plain member of the workspace, and the
// Super Agent has its own session. The same agent can have a different
// role in each workspace. The daemon loads prompts/<role>.md for it.
type SessionRole string

const (
	RoleLeader SessionRole = "leader"
	RoleMember SessionRole = "member"
	RoleSuper  SessionRole = "super"
)

// SessionEnsure asks the daemon to start or resume a session.
type SessionEnsure struct {
	SessionID  string         `json:"session_id"`
	Agent      string         `json:"agent"`
	Role       SessionRole    `json:"role"`
	Workspace  string         `json:"workspace"`
	Task       string         `json:"task"`
	AgentToken string         `json:"agent_token"`
	Resume     *ir.SessionRef `json:"resume,omitempty"`
}

// SessionDeliver delivers one rendered batch to a session.
type SessionDeliver struct {
	SessionID  string   `json:"session_id"`
	Input      ir.Input `json:"input"` // Input.Ref is the batch ID
	MessageIDs []string `json:"message_ids"`
	LastSeq    int64    `json:"last_seq"` // workspace seq of the last message in the batch
}

// SessionResolve answers an open request of a session.
type SessionResolve struct {
	SessionID string      `json:"session_id"`
	RequestID string      `json:"request_id"`
	Decision  ir.Decision `json:"decision"`
}

// SessionStop asks the daemon to stop a session gracefully.
type SessionStop struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

// SessionStateReport reports a change of session state.
type SessionStateReport struct {
	SessionID string          `json:"session_id"`
	State     ir.SessionState `json:"state"`
	Ref       *ir.SessionRef  `json:"ref,omitempty"`
	Reason    string          `json:"reason,omitempty"`
}

// Channel tells how the server must treat a session event.
type Channel string

const (
	ChannelReliable Channel = "reliable" // acknowledged, spooled while offline
	ChannelLive     Channel = "live"     // best-effort, dropped while offline
)

// SessionEvent carries one IR event.
type SessionEvent struct {
	Channel Channel  `json:"channel"`
	Event   ir.Event `json:"event"`
}

// Ack answers a command.
type Ack struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}
