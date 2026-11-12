package protocol

import (
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
)

// REST DTOs under /v1. These types are the contract with the CLI and the
// clients. The domain types in internal/core are separate.

type Machine struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	State    MachineState  `json:"state"`
	Online   bool          `json:"online"`
	LastSeen time.Time     `json:"last_seen"`
	Drivers  []DriverProbe `json:"drivers"` // the agent CLIs that the daemon detected
}

// MachineState is the approval state of a machine.
type MachineState string

const (
	MachinePending  MachineState = "pending"
	MachineApproved MachineState = "approved"
	MachineRevoked  MachineState = "revoked"
)

// MachineEnrollment is the body of POST /v1/machines/enroll (no token)
// and of POST /v1/machines (client token). The machine generated its token.
// It sends only the hash. The token itself goes only in the Authorization
// header of the daemon connection.
type MachineEnrollment struct {
	Name      string `json:"name"`
	TokenHash string `json:"token_hash"`
}

// MachineEnrolled is the answer to an enrollment.
type MachineEnrolled struct {
	MachineID string       `json:"machine_id"`
	State     MachineState `json:"state"`               // pending or approved
	PairCode  string       `json:"pair_code,omitempty"` // pending only
	PairUntil time.Time    `json:"pair_until,omitzero"`
}

// ApproveMachineRequest approves a pending machine by its pairing code.
type ApproveMachineRequest struct {
	Code string `json:"code"`
}

// Agent is a coding agent that a person set up on a machine.
type Agent struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	MachineID   string         `json:"machine_id"`
	Machine     string         `json:"machine"` // machine name
	Online      bool           `json:"online"`  // the machine is connected and has the driver
	Driver      ir.Kind        `json:"driver"`
	Role        string         `json:"role,omitempty"` // "" or "super"
	Dir         string         `json:"dir"`
	Model       string         `json:"model,omitempty"`
	Options     map[string]any `json:"options,omitempty"`
}

// CreateAgentRequest is the body of POST /v1/agents.
type CreateAgentRequest struct {
	Machine     string         `json:"machine"` // machine ID or name
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Driver      ir.Kind        `json:"driver"`
	Dir         string         `json:"dir"`
	Model       string         `json:"model,omitempty"`
	Options     map[string]any `json:"options,omitempty"`
	Super       bool           `json:"super,omitempty"`
}

// UpdateAgentRequest is the body of PATCH /v1/agents/{agent}. Absent
// fields stay as they are.
type UpdateAgentRequest struct {
	Description *string        `json:"description,omitempty"`
	Dir         *string        `json:"dir,omitempty"`
	Model       *string        `json:"model,omitempty"`
	Options     map[string]any `json:"options,omitempty"`
	Super       *bool          `json:"super,omitempty"`
}

// Job is one piece of work for one agent.
type Job struct {
	ID          string    `json:"id"`
	Agent       string    `json:"agent"` // agent name
	AgentID     string    `json:"agent_id"`
	SessionID   string    `json:"session_id"`
	RequestedBy string    `json:"requested_by"` // human | super
	Title       string    `json:"title"`
	Prompt      string    `json:"prompt"`
	State       string    `json:"state"` // queued | running | done | failed | cancelled
	Result      string    `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DispatchRequest is the body of POST /v1/jobs.
type DispatchRequest struct {
	Agent  string `json:"agent"` // agent name or ID
	Prompt string `json:"prompt"`
}

// SessionEventNote is the client push of one IR event of a session.
type SessionEventNote struct {
	SessionID string   `json:"session_id"`
	Agent     string   `json:"agent"`
	JobID     string   `json:"job_id,omitempty"`
	Super     bool     `json:"super,omitempty"` // the Super Agent session
	Event     ir.Event `json:"event"`
}

// Push is one record on the client stream (/v1/stream).
type Push struct {
	Event   string          `json:"event"` // agents.changed | machines.changed | job | session.event | super.message
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Workspace struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Leader  string   `json:"leader"` // agent ID
	Members []Member `json:"members"`
}

type Member struct {
	AgentID string `json:"agent_id"`
	Role    string `json:"role"` // leader | member
}

type Task struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	ParentID      string    `json:"parent_id,omitempty"`
	Depth         int       `json:"depth,omitempty"`
	Title         string    `json:"title"`
	Brief         string    `json:"brief,omitempty"`
	General       bool      `json:"general,omitempty"`
	State         string    `json:"state"`                    // open | running | summarizing | blocked | done | failed | cancelled
	BlockedReason string    `json:"blocked_reason,omitempty"` // ask | budget | fault
	Orchestrator  string    `json:"orchestrator,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at,omitzero"`
}

type Message struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	TaskID      string    `json:"task_id"`
	Seq         int64     `json:"seq"`
	Author      Author    `json:"author"`
	Kind        string    `json:"kind"`             // chat | report | summary | system
	Status      string    `json:"status,omitempty"` // report: progress | done | blocked | failed
	Mentions    []string  `json:"mentions,omitempty"`
	Text        string    `json:"text"`
	CreatedAt   time.Time `json:"created_at"`
}

type Author struct {
	Kind string `json:"kind"` // human | agent | super | system
	ID   string `json:"id,omitempty"`
}

type CreateWorkspaceRequest struct {
	Name   string `json:"name"`
	Leader string `json:"leader,omitempty"` // agent@machine
}

type AddMemberRequest struct {
	Agent  string `json:"agent"` // agent@machine
	Leader bool   `json:"leader,omitempty"`
}

type CreateTaskRequest struct {
	Brief string `json:"brief"`
}

// TextRequest is the body of the Super Agent conversation endpoints.
type TextRequest struct {
	Text string `json:"text"`
}

// SuperMessagesQuery reads the Super Agent conversation.
type SuperMessagesQuery struct {
	Since int64 `form:"since"`
	Limit int   `form:"limit"`
}

// PostMessageRequest has no author field. The server takes the author from
// the token.
type PostMessageRequest struct {
	TaskID     string   `json:"task_id,omitempty"` // empty: the general task
	Mentions   []string `json:"mentions,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Status     string   `json:"status,omitempty"`
	Checkpoint bool     `json:"checkpoint,omitempty"`
	Text       string   `json:"text"`
}

type ReadMessagesQuery struct {
	TaskID string `form:"task"`
	Since  int64  `form:"since"`
	Limit  int    `form:"limit"`
}

// Object is the result of a recall by ID (Chain of Recall): a summary of
// the object and the IDs of related objects.
type Object struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Summary string            `json:"summary"`
	Links   map[string]string `json:"links,omitempty"` // relation -> ID
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// Client stream (/v1/stream) event types.
const (
	StreamMessageCreated = "message.created"
	StreamTaskUpdated    = "task.updated"
	StreamSessionEvent   = "session.event"
	StreamMachineUpdated = "machine.updated"
	StreamSuperReply     = "super.reply"
)
