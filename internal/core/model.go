// Package core holds the domain model and the delivery engine. The Engine
// is the only writer of domain state. It reaches storage, daemons and
// clients only through the ports in ports.go.
package core

import (
	"encoding/json"
	"time"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// Owner is the person who owns an installation. v1 has one owner; the
// schema keeps owner_id so that team support can come later.
type Owner struct {
	ID        ID
	CreatedAt time.Time
}

// Machine is a host that runs "solax daemon".
type Machine struct {
	ID        ID
	OwnerID   ID
	Name      string
	State     MachineState
	TokenHash string    // SHA-256 of the token that the machine generated
	PairCode  string    // pending only: the code that a client approves
	PairUntil time.Time // pending only: the code expires
	LastSeen  time.Time
	Online    bool
	Slots     int // free session slots from the last heartbeat
	// Drivers are the coding agent CLIs that the daemon detected.
	Drivers []protocol.DriverProbe
}

// HasDriver reports whether the daemon detected a working driver of kind k.
func (m Machine) HasDriver(k ir.Kind) bool {
	for _, d := range m.Drivers {
		if d.Kind == k && d.OK {
			return true
		}
	}
	return false
}

// MachineState is the approval state of a machine.
type MachineState string

const (
	MachinePending  MachineState = "pending"  // enrolled, waits for a client to approve
	MachineApproved MachineState = "approved" // its token is valid
	MachineRevoked  MachineState = "revoked"
)

// PairTTL is how long a pairing code is valid.
const PairTTL = 24 * time.Hour

// Agent is a coding agent that a person set up on one machine. The server
// defines it; the daemon of the machine runs its sessions. The name is
// unique: the Super Agent routes jobs by name, with the description as the
// hint for what the agent is good at.
type Agent struct {
	ID          ID
	MachineID   ID
	Name        string
	Description string
	Driver      ir.Kind
	Role        AgentRole
	Dir         string
	Model       string
	Options     map[string]any // driver-specific, for example pi "thinking"
	Deleted     bool
	CreatedAt   time.Time
}

// AgentRole is the system role of an agent.
type AgentRole string

const (
	RoleWorker AgentRole = ""
	RoleSuper  AgentRole = "super"
)

// Workspace is a long-lived group of agents with a shared message log.
type Workspace struct {
	ID       ID
	OwnerID  ID
	Name     string
	LeaderID ID // agent ID; exactly one leader
	// Orchestrator is the default orchestrator plugin of new tasks. Empty
	// means the server default.
	Orchestrator string
	NextSeq      int64
	CreatedAt    time.Time
}

// MemberRole is the role of an agent in a workspace.
type MemberRole string

const (
	MemberLeader MemberRole = "leader"
	MemberPlain  MemberRole = "member"
)

// Member is an agent in a workspace.
type Member struct {
	WorkspaceID ID
	AgentID     ID
	Role        MemberRole
}

// TaskState is the state of a task.
type TaskState string

const (
	TaskOpen        TaskState = "open"
	TaskRunning     TaskState = "running"
	TaskSummarizing TaskState = "summarizing"
	TaskBlocked     TaskState = "blocked" // a pause; see BlockedReason
	TaskDone        TaskState = "done"
	TaskFailed      TaskState = "failed"
	TaskCancelled   TaskState = "cancelled"
)

// Terminal reports whether s is a final state. Final states are absorbing:
// no transition leaves them.
func (s TaskState) Terminal() bool {
	return s == TaskDone || s == TaskFailed || s == TaskCancelled
}

// BlockedReason tells why a task is blocked. When several apply, the
// strongest wins: fault > budget > ask.
type BlockedReason string

const (
	BlockedNone   BlockedReason = ""
	BlockedAsk    BlockedReason = "ask"    // waits for a human answer; the orchestrator keeps running
	BlockedBudget BlockedReason = "budget" // a budget bound was reached
	BlockedFault  BlockedReason = "fault"  // the orchestrator plugin faulted; it is stopped
)

// Task is one unit of work in a workspace, with its own message stream.
// Each workspace has a general task. A task may be the child of another
// task (a node of an orchestrator plugin).
type Task struct {
	ID            ID
	WorkspaceID   ID
	ParentID      ID     // empty for a root task
	ParentNode    string // the orchestrator action id that spawned it
	Depth         int    // 0 for a root task
	TargetAgentID ID     // the agent a spawned child task is for
	Title         string
	Brief         string
	General       bool
	State         TaskState
	BlockedReason BlockedReason
	Budget        Budget
	Spent         Spent

	// Orchestrator is the plugin name and OrchHash the pinned bundle. They
	// are fixed when the task is created. General tasks have none.
	Orchestrator string
	OrchHash     string
	OrchState    json.RawMessage // plugin state, strict JSON
	OrchLastSeq  int64           // last task event the orchestrator consumed

	// Rev increases on every state transition and orchestrator commit.
	// Writers that computed outside a transaction compare it on commit.
	Rev       int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Budget holds the hard limits of a task. When a limit is reached, the
// leader must write the summary.
type Budget struct {
	MaxAgentTurns       int
	MaxWallTime         time.Duration
	MaxHopsWithoutHuman int
}

// DefaultBudget is the budget of a new task.
var DefaultBudget = Budget{MaxAgentTurns: 40, MaxWallTime: 2 * time.Hour, MaxHopsWithoutHuman: 15}

// Spent is the used part of a budget.
type Spent struct {
	AgentTurns       int
	HopsWithoutHuman int
}

// AuthorKind is the kind of a message author.
type AuthorKind string

const (
	AuthorHuman  AuthorKind = "human"
	AuthorAgent  AuthorKind = "agent"
	AuthorSuper  AuthorKind = "super"
	AuthorSystem AuthorKind = "system"
)

// Author is the author of a message. It comes from the token, never from
// a request field.
type Author struct {
	Kind AuthorKind
	ID   ID
}

// MessageKind is the kind of a message.
type MessageKind string

const (
	MessageChat    MessageKind = "chat"
	MessageReport  MessageKind = "report"
	MessageSummary MessageKind = "summary"
	MessageSystem  MessageKind = "system"
)

// ReportStatus is the status of a report message.
type ReportStatus string

const (
	ReportProgress ReportStatus = "progress"
	ReportDone     ReportStatus = "done"
	ReportBlocked  ReportStatus = "blocked"
	ReportFailed   ReportStatus = "failed"
)

// Message is one append-only entry in a task stream.
type Message struct {
	ID          ID
	OwnerID     ID
	WorkspaceID ID    // empty for the Super Agent conversation
	TaskID      ID    // empty for the Super Agent conversation
	Seq         int64 // per workspace, or per owner for the Super Agent conversation
	Author      Author
	Kind        MessageKind
	Status      ReportStatus // report only
	Checkpoint  bool         // summary only: keep the task running
	Mentions    []ID         // agent IDs
	Text        string
	CreatedAt   time.Time
}

// Session is one agent process for one (member, task). The daemon owns the
// process. The server keeps its state and its native ref.
type Session struct {
	ID        ID
	AgentID   ID
	JobID     ID // the job of a worker session; empty for the Super Agent session
	State     ir.SessionState
	Ref       *ir.SessionRef
	Live      bool  // the daemon reports the process as running
	LastSeq   int64 // last stored IR event
	UpdatedAt time.Time
}

// SessionStopped is the state of a session that has no process and never
// had one, or whose process ended. An input wakes it again.
const SessionStopped ir.SessionState = "stopped"

// JobState is the state of a job.
type JobState string

const (
	JobQueued    JobState = "queued"  // waits for its session to take the prompt
	JobRunning   JobState = "running" // the agent works on it
	JobDone      JobState = "done"    // the last turn completed; Result holds the answer
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

// Terminal reports whether no more work happens without a new input.
func (s JobState) Terminal() bool { return s == JobDone || s == JobFailed || s == JobCancelled }

// Job is one piece of work for one agent. It runs in its own session. The
// caller gets the job ID at once and does not wait: when the turn of the
// agent completes, the job is done and the result goes back to whoever
// asked (the Super Agent inbox, or the person).
type Job struct {
	ID          ID
	OwnerID     ID
	AgentID     ID
	SessionID   ID
	RequestedBy AuthorKind // human or super
	Title       string
	Prompt      string
	State       JobState
	Result      string // the last agent message of the last turn
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Input is one entry in the inbox of a session. The delivery loop sends
// pending inputs in order. The ID is the ir.Input ref, so a redelivery
// after a lost ack is dropped by the daemon.
type Input struct {
	ID        ID
	SessionID ID
	Text      string
	Sent      bool
	CreatedAt time.Time
}

// Cursor tracks delivery of one task stream to one session.
type Cursor struct {
	SessionID    ID
	TaskID       ID
	DeliveredSeq int64
	ReadSeq      int64
}
