package protocol

import (
	"time"

	"github.com/austiecodes/solax/pkg/ir"
)

// REST DTOs under /v1. These types are the contract with the CLI and the
// clients. The domain types in internal/core are separate.

type Machine struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	State    MachineState `json:"state"`
	Online   bool         `json:"online"`
	LastSeen time.Time    `json:"last_seen"`
	Agents   []AgentInfo  `json:"agents"`
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

type Agent struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	MachineID string  `json:"machine_id"`
	Driver    ir.Kind `json:"driver"`
	Role      string  `json:"role,omitempty"`
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
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
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
