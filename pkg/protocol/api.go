package protocol

import (
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
)

// REST types under /v1. These types are the contract with the CLI and the
// clients. The domain types in internal/core are separate.

type Machine struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	State    MachineState   `json:"state"`
	Online   bool           `json:"online"`
	LastSeen time.Time      `json:"last_seen"`
	Runtimes []RuntimeProbe `json:"runtimes"` // the agent CLIs that the daemon detected
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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Instructions are the system prompt of its sessions, after the Solax
	// role prompt.
	Instructions string         `json:"instructions,omitempty"`
	MachineID    string         `json:"machine_id"`
	Machine      string         `json:"machine"` // machine name
	Online       bool           `json:"online"`  // the machine is connected and has the runtime
	Runtime      ir.Kind        `json:"runtime"`
	Role         string         `json:"role,omitempty"` // "" or "super"
	Dir          string         `json:"dir"`
	Model        string         `json:"model,omitempty"`
	Options      map[string]any `json:"options,omitempty"`
	// SessionID is the conversation session of the Super Agent, to read
	// its events (reasoning, tool calls). Empty for other agents.
	SessionID string `json:"session_id,omitempty"`
}

// CreateAgentRequest is the body of POST /v1/agents.
type CreateAgentRequest struct {
	Machine      string         `json:"machine"` // machine ID or name
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Instructions string         `json:"instructions,omitempty"`
	Runtime      ir.Kind        `json:"runtime"`
	Dir          string         `json:"dir"`
	Model        string         `json:"model,omitempty"`
	Options      map[string]any `json:"options,omitempty"`
	Super        bool           `json:"super,omitempty"`
}

// UpdateAgentRequest is the body of PATCH /v1/agents/{agent}. Absent
// fields stay as they are.
type UpdateAgentRequest struct {
	Description  *string        `json:"description,omitempty"`
	Instructions *string        `json:"instructions,omitempty"`
	Dir          *string        `json:"dir,omitempty"`
	Model        *string        `json:"model,omitempty"`
	Options      map[string]any `json:"options,omitempty"`
	Super        *bool          `json:"super,omitempty"`
}

// Task is one piece of work for one agent.
type Task struct {
	ID          string    `json:"id"`
	Agent       string    `json:"agent"` // agent name
	AgentID     string    `json:"agent_id"`
	SessionID   string    `json:"session_id"`
	RequestedBy string    `json:"requested_by"` // human | super: who created the task
	TurnBy      string    `json:"turn_by"`      // human | super: who started the current turn and gets its result
	Title       string    `json:"title"`
	Prompt      string    `json:"prompt"`
	State       string    `json:"state"` // queued | running | done | failed | cancelled
	Result      string    `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// FollowUpRequest is the body of POST /v1/tasks/:task/messages.
type FollowUpRequest struct {
	Text string `json:"text"`
	// Mode is "follow_up" (the default: a new turn after the current one)
	// or "steer" (guide the running turn).
	Mode string `json:"mode,omitempty"`
}

// DispatchRequest is the body of POST /v1/tasks.
type DispatchRequest struct {
	Agent  string `json:"agent"` // agent name or ID
	Prompt string `json:"prompt"`
}

// SessionEventNote is the client push of one IR event of a session.
type SessionEventNote struct {
	SessionID string   `json:"session_id"`
	Agent     string   `json:"agent"`
	TaskID    string   `json:"task_id,omitempty"`
	Super     bool     `json:"super,omitempty"` // the Super Agent session
	Event     ir.Event `json:"event"`
}

// Push is one record on the client stream (/v1/stream).
type Push struct {
	Event   string          `json:"event"` // agents.changed | machines.changed | task | session.event | super.message
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Message is one entry in the Super Agent conversation.
type Message struct {
	ID        string    `json:"id"`
	Seq       int64     `json:"seq"`
	Author    Author    `json:"author"`
	Kind      string    `json:"kind"` // chat | system
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type Author struct {
	Kind string `json:"kind"` // human | agent | super | system
	ID   string `json:"id,omitempty"`
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

// Object is the result of a recall by ID: a summary of
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

// Workflows.

// WorkflowMeta is the meta export of a workflow script.
type WorkflowMeta struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	WhenToUse   string          `json:"whenToUse,omitempty"`
	Phases      []WorkflowPhase `json:"phases,omitempty"`
}

// WorkflowPhase is one phase in WorkflowMeta.
type WorkflowPhase struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// Workflow is one run of a workflow script.
type Workflow struct {
	ID          string          `json:"id"`
	Name        string          `json:"name,omitempty"`
	Title       string          `json:"title"`
	RequestedBy string          `json:"requested_by"` // human | super
	ResumeFrom  string          `json:"resume_from,omitempty"`
	State       string          `json:"state"` // running | waiting | done | failed | cancelled
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	Logs        string          `json:"logs,omitempty"`
	Source      string          `json:"source,omitempty"` // only in one workflow
	Steps       []WorkflowStep  `json:"steps,omitempty"`  // only in one workflow
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// WorkflowStep is one wait of a workflow script.
type WorkflowStep struct {
	Key    string          `json:"key"`
	Kind   string          `json:"kind"` // agents | recall | task | reply | ask | sleep
	Target string          `json:"target,omitempty"`
	Phase  string          `json:"phase,omitempty"`
	Label  string          `json:"label,omitempty"`
	State  string          `json:"state"` // pending | done | failed
	Output json.RawMessage `json:"output,omitempty"`
}

// SavedWorkflow is a workflow script kept under a name.
type SavedWorkflow struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Meta        WorkflowMeta `json:"meta"`
	Source      string       `json:"source,omitempty"` // only in one saved workflow
	UpdatedAt   time.Time    `json:"updated_at"`
}

// WorkflowList is the answer of GET /v1/workflows.
type WorkflowList struct {
	Saved []SavedWorkflow `json:"saved"`
	Runs  []Workflow      `json:"runs"`
}

// WorkflowItem is the answer of GET /v1/workflows/:ref: a run (wfl_ ID) or
// a saved workflow (name).
type WorkflowItem struct {
	Run   *Workflow      `json:"run,omitempty"`
	Saved *SavedWorkflow `json:"saved,omitempty"`
	Runs  []Workflow     `json:"runs,omitempty"` // recent runs of a saved workflow
}

// StartWorkflowRequest is the body of POST /v1/workflows. Give Source, or
// Name to run a saved workflow.
type StartWorkflowRequest struct {
	Source string          `json:"source,omitempty"`
	Name   string          `json:"name,omitempty"`
	Args   json.RawMessage `json:"args,omitempty"`
	// Resume is a wfl_ ID: the new run reuses its finished steps while the
	// script makes the same calls.
	Resume string `json:"resume,omitempty"`
}

// SaveWorkflowRequest is the body of POST /v1/workflows/saved.
type SaveWorkflowRequest struct {
	Source string `json:"source"`
}

// AnswerRequest is the body of POST /v1/workflows/:workflow/answer.
type AnswerRequest struct {
	Choice string `json:"choice"`
	Text   string `json:"text,omitempty"`
}
